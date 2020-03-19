// Copyright © 2019 The Things Network Foundation, The Things Industries B.V.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package networkserver

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	pbtypes "github.com/gogo/protobuf/types"
	clusterauth "go.thethings.network/lorawan-stack/pkg/auth/cluster"
	"go.thethings.network/lorawan-stack/pkg/band"
	"go.thethings.network/lorawan-stack/pkg/cluster"
	"go.thethings.network/lorawan-stack/pkg/crypto"
	"go.thethings.network/lorawan-stack/pkg/crypto/cryptoutil"
	"go.thethings.network/lorawan-stack/pkg/encoding/lorawan"
	"go.thethings.network/lorawan-stack/pkg/errors"
	"go.thethings.network/lorawan-stack/pkg/events"
	"go.thethings.network/lorawan-stack/pkg/frequencyplans"
	"go.thethings.network/lorawan-stack/pkg/log"
	"go.thethings.network/lorawan-stack/pkg/tenant"
	"go.thethings.network/lorawan-stack/pkg/ttnpb"
	"go.thethings.network/lorawan-stack/pkg/types"
	"go.thethings.network/lorawan-stack/pkg/unique"
)

const (
	// recentUplinkCount is the maximum amount of recent uplinks stored per device.
	recentUplinkCount = 20

	// accumulationCapacity is the initial capacity of the accumulator.
	accumulationCapacity = 20

	// retransmissionWindow is the maximum delay between Rx2 end and an uplink retransmission.
	retransmissionWindow = 10 * time.Second

	// maxConfNbTrans is the maximum number of confirmed uplink retransmissions for pre-1.0.3 devices.
	maxConfNbTrans = 5
)

// UplinkDeduplicator represents an entity, that deduplicates uplinks and accumulates metadata.
type UplinkDeduplicator interface {
	// DeduplicateUplink deduplicates an uplink message for specified time.Duration.
	// DeduplicateUplink returns true if the uplink is not a duplicate or false and error, if any, otherwise.
	DeduplicateUplink(context.Context, *ttnpb.UplinkMessage, time.Duration) (bool, error)
	// AccumulatedMetadata returns accumulated metadata for specified uplink message and error, if any.
	AccumulatedMetadata(context.Context, *ttnpb.UplinkMessage) ([]*ttnpb.RxMetadata, error)
}

func (ns *NetworkServer) deduplicateUplink(ctx context.Context, up *ttnpb.UplinkMessage) (bool, error) {
	ok, err := ns.uplinkDeduplicator.DeduplicateUplink(ctx, up, ns.collectionWindow(ctx))
	if err != nil {
		log.FromContext(ctx).Error("Failed to deduplicate uplink")
		return false, err
	}
	if !ok {
		log.FromContext(ctx).Debug("Dropped duplicate uplink")
		return false, nil
	}
	registerReceiveUniqueUplink(ctx, up)
	return true, nil
}

func resetsFCnt(dev *ttnpb.EndDevice, defaults ttnpb.MACSettings) bool {
	if dev.MACSettings != nil && dev.MACSettings.ResetsFCnt != nil {
		return dev.MACSettings.ResetsFCnt.Value
	}
	if defaults.ResetsFCnt != nil {
		return defaults.ResetsFCnt.Value
	}
	return false
}

// transmissionNumber returns the number of the transmission up would represent if appended to ups
// and the time of the last transmission of phyPayload in ups, if such is found.
func transmissionNumber(phyPayload []byte, ups ...*ttnpb.UplinkMessage) (uint32, time.Time, error) {
	if len(phyPayload) < 4 {
		return 0, time.Time{}, errRawPayloadTooShort.New()
	}

	nb := uint32(1)
	var lastTrans time.Time
	for i := len(ups) - 1; i >= 0; i-- {
		up := ups[i]
		if len(up.RawPayload) < 4 {
			return 0, time.Time{}, errRawPayloadTooShort.New()
		}
		if !bytes.Equal(phyPayload[:len(phyPayload)-4], up.RawPayload[:len(up.RawPayload)-4]) {
			break
		}
		nb++
		if up.ReceivedAt.After(lastTrans) {
			lastTrans = up.ReceivedAt
		}
	}
	return nb, lastTrans, nil
}

func maxTransmissionNumber(ver ttnpb.MACVersion, confirmed bool, nbTrans uint32) uint32 {
	if !confirmed {
		return nbTrans
	}
	if ver.Compare(ttnpb.MAC_V1_0_3) < 0 {
		return maxConfNbTrans
	}
	return nbTrans
}

func maxRetransmissionDelay(rxDelay ttnpb.RxDelay) time.Duration {
	return rxDelay.Duration() + time.Second + retransmissionWindow
}

func fCntResetGap(last, recv uint32) uint32 {
	if math.MaxUint32-last < recv {
		return last + recv
	} else {
		return math.MaxUint32
	}
}

type macHandler func(context.Context, *ttnpb.EndDevice, *ttnpb.UplinkMessage) ([]events.DefinitionDataClosure, error)

func makeDeferredMACHandler(dev *ttnpb.EndDevice, f macHandler) macHandler {
	queuedLength := len(dev.MACState.QueuedResponses)
	return func(ctx context.Context, dev *ttnpb.EndDevice, up *ttnpb.UplinkMessage) ([]events.DefinitionDataClosure, error) {
		switch n := len(dev.MACState.QueuedResponses); {
		case n < queuedLength:
			return nil, errCorruptedMACState.New()
		case n == queuedLength:
			return f(ctx, dev, up)
		default:
			tail := append(dev.MACState.QueuedResponses[queuedLength:0:0], dev.MACState.QueuedResponses[queuedLength:]...)
			dev.MACState.QueuedResponses = dev.MACState.QueuedResponses[:queuedLength]
			evs, err := f(ctx, dev, up)
			dev.MACState.QueuedResponses = append(dev.MACState.QueuedResponses, tail...)
			return evs, err
		}
	}
}

type matchedDevice struct {
	phy band.Band

	Context                  context.Context
	ChannelIndex             uint8
	DataRateIndex            ttnpb.DataRateIndex
	DeferredMACHandlers      []macHandler
	Device                   *ttnpb.EndDevice
	FCnt                     uint32
	FCntReset                bool
	NbTrans                  uint32
	Pending                  bool
	QueuedApplicationUplinks []*ttnpb.ApplicationUp
	QueuedEvents             []events.DefinitionDataClosure
	SetPaths                 []string
}

func (d *matchedDevice) deferMACHandler(f macHandler) {
	d.DeferredMACHandlers = append(d.DeferredMACHandlers, makeDeferredMACHandler(d.Device, f))
}

type contextualEndDevice struct {
	context.Context
	*ttnpb.EndDevice
}

// matchAndHandleDataUplink tries to match the data uplink message with a device and returns the matched device.
func (ns *NetworkServer) matchAndHandleDataUplink(up *ttnpb.UplinkMessage, deduplicated bool, devs ...contextualEndDevice) (*matchedDevice, error) {
	if len(up.RawPayload) < 4 {
		return nil, errRawPayloadTooShort.New()
	}
	pld := up.Payload.GetMACPayload()

	type device struct {
		matchedDevice
		gap                        uint32
		pendingApplicationDownlink *ttnpb.ApplicationDownlink
	}
	matches := make([]device, 0, len(devs))
	for _, dev := range devs {
		if dev.Multicast {
			continue
		}
		dev := dev
		ctx := dev.Context

		logger := log.FromContext(ctx).WithField("device_uid", unique.ID(ctx, dev.EndDeviceIdentifiers))
		ctx = log.NewContext(ctx, logger)

		_, phy, err := getDeviceBandVersion(dev.EndDevice, ns.FrequencyPlans)
		if err != nil {
			logger.WithError(err).Warn("Failed to get device's versioned band, skip")
			continue
		}

		drIdx, dr, ok := phy.FindUplinkDataRate(up.Settings.DataRate)
		if !ok {
			logger.WithError(err).Debug("Data rate not found in PHY, skip")
			continue
		}

		pendingApplicationDownlink := dev.GetMACState().GetPendingApplicationDownlink()

		uplinkDwellTime := func(macState *ttnpb.MACState) bool {
			if macState.CurrentParameters.UplinkDwellTime != nil {
				return macState.CurrentParameters.UplinkDwellTime.Value
			}
			// Assume no dwell time if current value unknown.
			return false
		}

		if !pld.Ack &&
			dev.PendingSession != nil &&
			dev.PendingMACState != nil &&
			dev.PendingSession.DevAddr == pld.DevAddr &&
			(!dev.PendingMACState.LoRaWANVersion.IgnoreUplinksExceedingLengthLimit() || len(up.RawPayload)-5 <= int(dr.MaxMACPayloadSize(uplinkDwellTime(dev.PendingMACState)))) {
			logger := logger.WithFields(log.Fields(
				"mac_version", dev.PendingMACState.LoRaWANVersion,
				"pending_session", true,
				"f_cnt_gap", pld.FCnt,
				"full_f_cnt_up", pld.FCnt,
				"transmission", 1,
			))
			ctx := log.NewContext(ctx, logger)

			pendingDev := dev.EndDevice
			if dev.Session != nil && dev.MACState != nil && dev.Session.DevAddr == pld.DevAddr {
				logger.Error("Same DevAddr was assigned to a device in two consecutive sessions")
				pendingDev = copyEndDevice(dev.EndDevice)
			}
			pendingDev.MACState = pendingDev.PendingMACState
			pendingDev.PendingMACState = nil

			matches = append(matches, device{
				matchedDevice: matchedDevice{
					phy:           phy,
					Context:       ctx,
					DataRateIndex: drIdx,
					Device:        pendingDev,
					FCnt:          pld.FCnt,
					NbTrans:       1,
					Pending:       true,
				},
				gap:                        pld.FCnt,
				pendingApplicationDownlink: pendingApplicationDownlink,
			})
		}

		switch {
		case dev.Session == nil,
			dev.MACState == nil,
			dev.Session.DevAddr != pld.DevAddr,
			dev.MACState.LoRaWANVersion.IgnoreUplinksExceedingLengthLimit() && len(up.RawPayload)-5 > int(dr.MaxMACPayloadSize(uplinkDwellTime(dev.MACState))):
			continue

		case pld.Ack && len(dev.MACState.RecentDownlinks) == 0:
			logger.Debug("Uplink contains ACK, but no downlink was sent to device, skip")
			continue
		}

		supports32BitFCnt := true
		if dev.GetMACSettings().GetSupports32BitFCnt() != nil {
			supports32BitFCnt = dev.MACSettings.Supports32BitFCnt.Value
		} else if ns.defaultMACSettings.GetSupports32BitFCnt() != nil {
			supports32BitFCnt = ns.defaultMACSettings.Supports32BitFCnt.Value
		}

		fCnt := pld.FCnt
		switch {
		case !supports32BitFCnt, fCnt >= dev.Session.LastFCntUp, fCnt == 0 && dev.Session.LastFCntUp == 0:
		case fCnt > dev.Session.LastFCntUp&0xffff:
			fCnt |= dev.Session.LastFCntUp &^ 0xffff
		case dev.Session.LastFCntUp < 0xffff0000:
			fCnt |= (dev.Session.LastFCntUp + 0x10000) &^ 0xffff
		}

		maxNbTrans := maxTransmissionNumber(dev.MACState.LoRaWANVersion, up.Payload.MType == ttnpb.MType_CONFIRMED_UP, dev.MACState.CurrentParameters.ADRNbTrans)
		logger = logger.WithFields(log.Fields(
			"last_f_cnt_up", dev.Session.LastFCntUp,
			"mac_version", dev.MACState.LoRaWANVersion,
			"max_transmissions", maxNbTrans,
			"pending_session", false,
			"supports_32_bit_f_cnt", true,
		))
		ctx = log.NewContext(ctx, logger)

		if fCnt == dev.Session.LastFCntUp && len(dev.MACState.RecentUplinks) > 0 {
			nbTrans, lastAt, err := transmissionNumber(up.RawPayload, dev.MACState.RecentUplinks...)
			if err != nil {
				logger.WithError(err).Error("Failed to determine transmission number")
				continue
			}
			logger = logger.WithFields(log.Fields(
				"f_cnt_gap", 0,
				"f_cnt_reset", false,
				"full_f_cnt_up", dev.Session.LastFCntUp,
				"transmission", nbTrans,
			))
			ctx = log.NewContext(ctx, logger)
			if nbTrans < 2 || lastAt.IsZero() {
				logger.Debug("Repeated FCnt value, but frame is not a retransmission, skip")
				continue
			}

			maxDelay := maxRetransmissionDelay(dev.MACState.CurrentParameters.Rx1Delay)
			delay := up.ReceivedAt.Sub(lastAt)

			logger = logger.WithFields(log.Fields(
				"last_transmission_at", lastAt,
				"max_retransmission_delay", maxDelay,
				"retransmission_delay", delay,
			))
			ctx = log.NewContext(ctx, logger)

			if delay > maxDelay {
				logger.Warn("Retransmission delay exceeds maximum, skip")
				continue
			}
			if nbTrans > maxNbTrans {
				logger.Warn("Transmission number exceeds maximum, skip")
				continue
			}
			matches = append(matches, device{
				matchedDevice: matchedDevice{
					phy:           phy,
					Context:       ctx,
					DataRateIndex: drIdx,
					Device:        dev.EndDevice,
					FCnt:          dev.Session.LastFCntUp,
					NbTrans:       nbTrans,
				},
				pendingApplicationDownlink: pendingApplicationDownlink,
			})
			continue
		}

		if fCnt < dev.Session.LastFCntUp {
			if !resetsFCnt(dev.EndDevice, ns.defaultMACSettings) {
				logger.Debug("FCnt too low, skip")
				continue
			}

			macState, err := newMACState(dev.EndDevice, ns.FrequencyPlans, ns.defaultMACSettings)
			if err != nil {
				logger.WithError(err).Warn("Failed to generate new MAC state")
				continue
			}
			if macState.LoRaWANVersion.HasMaxFCntGap() && uint(pld.FCnt) > phy.MaxFCntGap {
				continue
			}
			dev.MACState = macState

			gap := fCntResetGap(dev.Session.LastFCntUp, pld.FCnt)
			matches = append(matches, device{
				matchedDevice: matchedDevice{
					phy: phy,
					Context: log.NewContextWithFields(ctx, log.Fields(
						"f_cnt_gap", gap,
						"f_cnt_reset", true,
						"full_f_cnt_up", pld.FCnt,
						"transmission", 1,
					)),
					DataRateIndex: drIdx,
					Device:        dev.EndDevice,
					FCnt:          pld.FCnt,
					FCntReset:     true,
					NbTrans:       1,
				},
				gap:                        gap,
				pendingApplicationDownlink: pendingApplicationDownlink,
			})
			continue
		}

		logger = logger.WithField("transmission", 1)
		ctx = log.NewContext(ctx, logger)

		if fCnt != pld.FCnt && resetsFCnt(dev.EndDevice, ns.defaultMACSettings) {
			macState, err := newMACState(dev.EndDevice, ns.FrequencyPlans, ns.defaultMACSettings)
			if err != nil {
				logger.WithError(err).Warn("Failed to generate new MAC state")
				continue
			}
			if !macState.LoRaWANVersion.HasMaxFCntGap() || uint(pld.FCnt) <= phy.MaxFCntGap {
				dev := copyEndDevice(dev.EndDevice)
				dev.MACState = macState

				gap := fCntResetGap(dev.Session.LastFCntUp, pld.FCnt)
				matches = append(matches, device{
					matchedDevice: matchedDevice{
						phy: phy,
						Context: log.NewContextWithFields(ctx, log.Fields(
							"f_cnt_gap", gap,
							"f_cnt_reset", true,
							"full_f_cnt_up", pld.FCnt,
						)),
						DataRateIndex: drIdx,
						Device:        dev,
						FCnt:          pld.FCnt,
						FCntReset:     true,
						NbTrans:       1,
					},
					gap:                        gap,
					pendingApplicationDownlink: pendingApplicationDownlink,
				})
			}
		}

		gap := fCnt - dev.Session.LastFCntUp
		logger = logger.WithFields(log.Fields(
			"f_cnt_gap", gap,
			"f_cnt_reset", false,
			"full_f_cnt_up", fCnt,
		))
		ctx = log.NewContext(ctx, logger)

		if fCnt == math.MaxUint32 {
			logger.Debug("FCnt too high, skip")
			continue
		}
		if dev.MACState.LoRaWANVersion.HasMaxFCntGap() && uint(gap) > phy.MaxFCntGap {
			logger.Debug("FCnt gap too high, skip")
			continue
		}
		matches = append(matches, device{
			matchedDevice: matchedDevice{
				phy:           phy,
				Context:       ctx,
				DataRateIndex: drIdx,
				Device:        dev.EndDevice,
				FCnt:          fCnt,
				NbTrans:       1,
			},
			gap:                        gap,
			pendingApplicationDownlink: pendingApplicationDownlink,
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].gap != matches[j].gap {
			return matches[i].gap < matches[j].gap
		}
		if matches[i].FCntReset != matches[j].FCntReset {
			return matches[j].FCntReset
		}
		return matches[i].FCnt < matches[j].FCnt
	})

matchLoop:
	for i, match := range matches {
		ctx := match.Context
		logger := log.FromContext(ctx).WithField("match_attempt", i)

		session := match.Device.Session
		if match.Pending {
			session = match.Device.PendingSession

			if match.Device.MACState.PendingJoinRequest == nil {
				logger.Error("Pending join-request missing")
				continue
			}
			match.Device.MACState.CurrentParameters.Rx1Delay = match.Device.MACState.PendingJoinRequest.RxDelay
			match.Device.MACState.CurrentParameters.Rx1DataRateOffset = match.Device.MACState.PendingJoinRequest.DownlinkSettings.Rx1DROffset
			match.Device.MACState.CurrentParameters.Rx2DataRateIndex = match.Device.MACState.PendingJoinRequest.DownlinkSettings.Rx2DR
			if match.Device.MACState.PendingJoinRequest.DownlinkSettings.OptNeg && match.Device.LoRaWANVersion.Compare(ttnpb.MAC_V1_1) >= 0 {
				// The version will be further negotiated via RekeyInd/RekeyConf
				match.Device.MACState.LoRaWANVersion = ttnpb.MAC_V1_1
			}
			if match.Device.MACState.PendingJoinRequest.CFList != nil {
				switch match.Device.MACState.PendingJoinRequest.CFList.Type {
				case ttnpb.CFListType_FREQUENCIES:
					for _, freq := range match.Device.MACState.PendingJoinRequest.CFList.Freq {
						if freq == 0 {
							break
						}
						match.Device.MACState.CurrentParameters.Channels = append(match.Device.MACState.CurrentParameters.Channels, &ttnpb.MACParameters_Channel{
							UplinkFrequency:   uint64(freq * 100),
							DownlinkFrequency: uint64(freq * 100),
							MaxDataRateIndex:  ttnpb.DataRateIndex(match.phy.MaxADRDataRateIndex),
							EnableUplink:      true,
						})
					}

				case ttnpb.CFListType_CHANNEL_MASKS:
					if len(match.Device.MACState.CurrentParameters.Channels) != len(match.Device.MACState.PendingJoinRequest.CFList.ChMasks) {
						logger.Debug("Device channel length does not equal length of join-request ChMasks, skip")
						continue matchLoop
					}
					for i, m := range match.Device.MACState.PendingJoinRequest.CFList.ChMasks {
						if m {
							continue
						}
						if match.Device.MACState.CurrentParameters.Channels[i] == nil {
							logger.WithField("channel_index", i).Debug("Device channel present in join-request ChMasks is not defined, skip")
							continue matchLoop
						}
						match.Device.MACState.CurrentParameters.Channels[i].EnableUplink = m
					}
				}
			}
		}
		if session.FNwkSIntKey == nil || len(session.FNwkSIntKey.Key) == 0 {
			logger.Warn("Device missing FNwkSIntKey in registry, skip")
			continue
		}
		fNwkSIntKey, err := cryptoutil.UnwrapAES128Key(ctx, *session.FNwkSIntKey, ns.KeyVault)
		if err != nil {
			logger.WithField("kek_label", session.FNwkSIntKey.KEKLabel).WithError(err).Warn("Failed to unwrap FNwkSIntKey, skip")
			continue
		}

		macBuf := pld.FOpts
		if len(macBuf) == 0 && pld.FPort == 0 {
			macBuf = pld.FRMPayload
		}
		if len(macBuf) > 0 && (len(pld.FOpts) == 0 || match.Device.MACState.LoRaWANVersion.EncryptFOpts()) {
			if session.NwkSEncKey == nil || len(session.NwkSEncKey.Key) == 0 {
				logger.Warn("Device missing NwkSEncKey in registry, skip")
				continue
			}
			key, err := cryptoutil.UnwrapAES128Key(ctx, *session.NwkSEncKey, ns.KeyVault)
			if err != nil {
				logger.WithField("kek_label", session.NwkSEncKey.KEKLabel).WithError(err).Warn("Failed to unwrap NwkSEncKey, skip")
				continue
			}
			macBuf, err = crypto.DecryptUplink(key, pld.DevAddr, match.FCnt, macBuf)
			if err != nil {
				logger.WithError(err).Warn("Failed to decrypt uplink, skip")
				continue
			}
		}

		if match.NbTrans > 1 {
			match.Device.MACState.PendingRequests = nil
		}
		var cmds []*ttnpb.MACCommand
		for r := bytes.NewReader(macBuf); r.Len() > 0; {
			cmd := &ttnpb.MACCommand{}
			if err := lorawan.DefaultMACCommands.ReadUplink(match.phy, r, cmd); err != nil {
				logger.WithFields(log.Fields(
					"bytes_left", r.Len(),
					"mac_count", len(cmds),
				)).WithError(err).Warn("Failed to read MAC command")
				break
			}
			logger := logger.WithField("cid", cmd.CID)
			logger.Debug("Read MAC command")
			def, ok := lorawan.DefaultMACCommands[cmd.CID]
			switch {
			case ok && !def.InitiatedByDevice && (match.Pending || match.FCntReset):
				logger.Debug("Received MAC command answer after MAC state reset, skip")
				continue matchLoop
			case ok && match.NbTrans > 1 && !lorawan.DefaultMACCommands[cmd.CID].InitiatedByDevice:
				logger.Debug("Skip processing of MAC command not initiated by the device in a retransmission")
				continue
			}
			cmds = append(cmds, cmd)
		}
		logger = logger.WithField("mac_count", len(cmds))
		ctx = log.NewContext(ctx, logger)

		if pld.ClassB {
			switch {
			case !match.Device.SupportsClassB:
				logger.Debug("Ignore class B bit in uplink, since device does not support class B")

			case match.Device.MACState.CurrentParameters.PingSlotFrequency == 0:
				logger.Debug("Ignore class B bit in uplink, since ping slot frequency is not known")

			case match.Device.MACState.CurrentParameters.PingSlotDataRateIndexValue == nil:
				logger.Debug("Ignore class B bit in uplink, since ping slot data rate index is not known")

			case match.Device.MACState.PingSlotPeriodicity == nil:
				logger.Debug("Ignore class B bit in uplink, since ping slot periodicity is not known")

			case match.Device.MACState.DeviceClass != ttnpb.CLASS_B:
				logger.WithField("previous_class", match.Device.MACState.DeviceClass).Debug("Switch device class to class B")
				match.QueuedEvents = append(match.QueuedEvents, evtClassBSwitch.BindData(match.Device.MACState.DeviceClass))
				match.Device.MACState.DeviceClass = ttnpb.CLASS_B
			}
		} else if match.Device.MACState.DeviceClass == ttnpb.CLASS_B {
			if match.Device.LoRaWANVersion.Compare(ttnpb.MAC_V1_1) < 0 && match.Device.SupportsClassC {
				match.QueuedEvents = append(match.QueuedEvents, evtClassCSwitch.BindData(ttnpb.CLASS_B))
				match.Device.MACState.DeviceClass = ttnpb.CLASS_C
			} else {
				match.QueuedEvents = append(match.QueuedEvents, evtClassASwitch.BindData(ttnpb.CLASS_B))
				match.Device.MACState.DeviceClass = ttnpb.CLASS_A
			}
		}

		match.Device.MACState.QueuedResponses = match.Device.MACState.QueuedResponses[:0]
	macLoop:
		for len(cmds) > 0 {
			var cmd *ttnpb.MACCommand
			cmd, cmds = cmds[0], cmds[1:]
			logger := logger.WithField("cid", cmd.CID)
			ctx := log.NewContext(ctx, logger)

			logger.Debug("Handle MAC command")

			var evs []events.DefinitionDataClosure
			var err error
			switch cmd.CID {
			case ttnpb.CID_RESET:
				evs, err = handleResetInd(ctx, match.Device, cmd.GetResetInd(), ns.FrequencyPlans, ns.defaultMACSettings)
			case ttnpb.CID_LINK_CHECK:
				if !deduplicated {
					match.deferMACHandler(handleLinkCheckReq)
					continue macLoop
				}
				evs, err = handleLinkCheckReq(ctx, match.Device, up)
			case ttnpb.CID_LINK_ADR:
				pld := cmd.GetLinkADRAns()
				dupCount := 0
				if match.Device.MACState.LoRaWANVersion.Compare(ttnpb.MAC_V1_0_2) >= 0 && match.Device.MACState.LoRaWANVersion.Compare(ttnpb.MAC_V1_1) < 0 {
					for _, dup := range cmds {
						if dup.CID != ttnpb.CID_LINK_ADR {
							break
						}
						if *dup.GetLinkADRAns() != *pld {
							err = errInvalidPayload
							break
						}
						dupCount++
					}
				}
				if err != nil {
					break
				}
				cmds = cmds[dupCount:]
				evs, err = handleLinkADRAns(ctx, match.Device, pld, uint(dupCount), ns.FrequencyPlans)
			case ttnpb.CID_DUTY_CYCLE:
				evs, err = handleDutyCycleAns(ctx, match.Device)
			case ttnpb.CID_RX_PARAM_SETUP:
				evs, err = handleRxParamSetupAns(ctx, match.Device, cmd.GetRxParamSetupAns())
			case ttnpb.CID_DEV_STATUS:
				evs, err = handleDevStatusAns(ctx, match.Device, cmd.GetDevStatusAns(), session.LastFCntUp, up.ReceivedAt)
				if err == nil {
					match.SetPaths = append(match.SetPaths,
						"battery_percentage",
						"downlink_margin",
						"last_dev_status_received_at",
						"power_state",
					)
				}
			case ttnpb.CID_NEW_CHANNEL:
				evs, err = handleNewChannelAns(ctx, match.Device, cmd.GetNewChannelAns())
			case ttnpb.CID_RX_TIMING_SETUP:
				evs, err = handleRxTimingSetupAns(ctx, match.Device)
			case ttnpb.CID_TX_PARAM_SETUP:
				evs, err = handleTxParamSetupAns(ctx, match.Device)
			case ttnpb.CID_DL_CHANNEL:
				evs, err = handleDLChannelAns(ctx, match.Device, cmd.GetDLChannelAns())
			case ttnpb.CID_REKEY:
				evs, err = handleRekeyInd(ctx, match.Device, cmd.GetRekeyInd())
			case ttnpb.CID_ADR_PARAM_SETUP:
				evs, err = handleADRParamSetupAns(ctx, match.Device)
			case ttnpb.CID_DEVICE_TIME:
				if !deduplicated {
					match.deferMACHandler(handleDeviceTimeReq)
					continue macLoop
				}
				evs, err = handleDeviceTimeReq(ctx, match.Device, up)
			case ttnpb.CID_REJOIN_PARAM_SETUP:
				evs, err = handleRejoinParamSetupAns(ctx, match.Device, cmd.GetRejoinParamSetupAns())
			case ttnpb.CID_PING_SLOT_INFO:
				evs, err = handlePingSlotInfoReq(ctx, match.Device, cmd.GetPingSlotInfoReq())
			case ttnpb.CID_PING_SLOT_CHANNEL:
				evs, err = handlePingSlotChannelAns(ctx, match.Device, cmd.GetPingSlotChannelAns())
			case ttnpb.CID_BEACON_TIMING:
				evs, err = handleBeaconTimingReq(ctx, match.Device)
			case ttnpb.CID_BEACON_FREQ:
				evs, err = handleBeaconFreqAns(ctx, match.Device, cmd.GetBeaconFreqAns())
			case ttnpb.CID_DEVICE_MODE:
				evs, err = handleDeviceModeInd(ctx, match.Device, cmd.GetDeviceModeInd())
			default:
				logger.Warn("Unknown MAC command received, skip the rest")
				break macLoop
			}
			if err != nil {
				logger.WithError(err).Debug("Failed to process MAC command")
				break macLoop
			}
			match.QueuedEvents = append(match.QueuedEvents, evs...)
		}
		if n := len(match.Device.MACState.PendingRequests); n > 0 {
			logger.WithField("unanswered_request_count", n).Warn("MAC command buffer not fully answered")
			match.Device.MACState.PendingRequests = match.Device.MACState.PendingRequests[:0]
		}

		if match.Pending {
			if match.Device.MACState.LoRaWANVersion.Compare(ttnpb.MAC_V1_1) < 0 {
				match.Device.EndDeviceIdentifiers.DevAddr = &pld.DevAddr
				match.Device.MACState.PendingJoinRequest = nil
				match.Device.Session = match.Device.PendingSession
				match.Device.PendingSession = nil
				match.Device.PendingMACState = nil
			} else if match.Device.PendingSession != nil || match.Device.PendingMACState != nil || match.Device.MACState.PendingJoinRequest != nil {
				logger.Debug("No RekeyInd received for LoRaWAN 1.1+ device, skip")
				continue matchLoop
			}
			match.SetPaths = append(match.SetPaths, "ids.dev_addr")
		} else if match.Device.PendingSession != nil || match.Device.PendingMACState != nil {
			// TODO: Notify AS of session recovery(https://github.com/TheThingsNetwork/lorawan-stack/issues/594)
			match.Device.PendingMACState = nil
			match.Device.PendingSession = nil
		}

		chIdx, err := searchUplinkChannel(up.Settings.Frequency, match.Device.MACState)
		if err != nil {
			logger.WithError(err).Debug("Failed to determine channel index of uplink, skip")
			continue
		}
		logger = logger.WithField("channel_index", chIdx)
		ctx = log.NewContext(ctx, logger)
		match.ChannelIndex = chIdx

		var computedMIC [4]byte
		if match.Device.MACState.LoRaWANVersion.Compare(ttnpb.MAC_V1_1) < 0 {
			computedMIC, err = crypto.ComputeLegacyUplinkMIC(
				fNwkSIntKey,
				pld.DevAddr,
				match.FCnt,
				up.RawPayload[:len(up.RawPayload)-4],
			)
		} else {
			if match.Device.Session.SNwkSIntKey == nil || len(match.Device.Session.SNwkSIntKey.Key) == 0 {
				logger.Warn("Device missing SNwkSIntKey in registry, skip")
				continue
			}

			var sNwkSIntKey types.AES128Key
			sNwkSIntKey, err = cryptoutil.UnwrapAES128Key(ctx, *match.Device.Session.SNwkSIntKey, ns.KeyVault)
			if err != nil {
				logger.WithField("kek_label", match.Device.Session.SNwkSIntKey.KEKLabel).WithError(err).Warn("Failed to unwrap SNwkSIntKey, skip")
				continue
			}

			var confFCnt uint32
			if pld.Ack {
				confFCnt = match.Device.Session.LastConfFCntDown
			}
			computedMIC, err = crypto.ComputeUplinkMIC(
				sNwkSIntKey,
				fNwkSIntKey,
				confFCnt,
				uint8(match.DataRateIndex),
				chIdx,
				pld.DevAddr,
				match.FCnt,
				up.RawPayload[:len(up.RawPayload)-4],
			)
		}
		if err != nil {
			logger.WithError(err).Error("Failed to compute MIC")
			continue
		}
		if !bytes.Equal(up.Payload.MIC, computedMIC[:]) {
			logger.Debug("MIC mismatch")
			continue
		}

		if match.pendingApplicationDownlink != nil {
			asUp := &ttnpb.ApplicationUp{
				EndDeviceIdentifiers: ttnpb.EndDeviceIdentifiers{
					DevAddr:                &pld.DevAddr,
					JoinEUI:                match.Device.JoinEUI,
					DevEUI:                 match.Device.DevEUI,
					ApplicationIdentifiers: match.Device.ApplicationIdentifiers,
					DeviceID:               match.Device.DeviceID,
				},
				CorrelationIDs: append(match.pendingApplicationDownlink.CorrelationIDs, up.CorrelationIDs...),
			}
			if pld.Ack && !match.Pending && !match.FCntReset && match.NbTrans == 1 {
				asUp.Up = &ttnpb.ApplicationUp_DownlinkAck{
					DownlinkAck: match.pendingApplicationDownlink,
				}
			} else {
				asUp.Up = &ttnpb.ApplicationUp_DownlinkNack{
					DownlinkNack: match.pendingApplicationDownlink,
				}
			}
			match.QueuedApplicationUplinks = append(match.QueuedApplicationUplinks, asUp)
			match.Device.MACState.PendingApplicationDownlink = nil
		}
		if match.Pending || match.FCntReset {
			match.Device.Session.StartedAt = up.ReceivedAt
		}
		match.Device.MACState.RxWindowsAvailable = true
		match.Device.Session.LastFCntUp = match.FCnt
		match.SetPaths = append(match.SetPaths,
			"mac_state",
			"pending_mac_state",
			"pending_session",
			"session",
		)
		return &match.matchedDevice, nil
	}
	return nil, errDeviceNotFound.New()
}

// MACHandler defines the behavior of a MAC command on a device.
type MACHandler func(ctx context.Context, dev *ttnpb.EndDevice, pld []byte, up *ttnpb.UplinkMessage) error

func appendRecentUplink(recent []*ttnpb.UplinkMessage, up *ttnpb.UplinkMessage, window int) []*ttnpb.UplinkMessage {
	recent = append(recent, up)
	if len(recent) > window {
		recent = recent[len(recent)-window:]
	}
	return recent
}

var handleDataUplinkGetPaths = [...]string{
	"frequency_plan_id",
	"last_dev_status_received_at",
	"lorawan_phy_version",
	"lorawan_version",
	"mac_settings",
	"mac_state",
	"multicast",
	"pending_mac_state",
	"pending_session",
	"recent_adr_uplinks",
	"recent_uplinks",
	"session",
	"supports_class_b",
	"supports_class_c",
	"supports_join",
}

func (ns *NetworkServer) handleDataUplink(ctx context.Context, up *ttnpb.UplinkMessage) (err error) {
	defer func() {
		if err != nil {
			registerDropDataUplink(ctx, up, err)
		}
	}()
	pld := up.Payload.GetMACPayload()

	logger := log.FromContext(ctx).WithFields(log.Fields(
		"ack", pld.Ack,
		"adr", pld.ADR,
		"adr_ack_req", pld.ADRAckReq,
		"class_b", pld.ClassB,
		"dev_addr", pld.DevAddr,
		"f_opts_len", len(pld.FOpts),
		"f_port", pld.FPort,
		"frequency", up.Settings.Frequency,
		"uplink_f_cnt", pld.FCnt,
	))
	ctx = log.NewContext(ctx, logger)

	if !ns.enterpriseConfig.SwitchPeeringTenantContext && tenant.FromContext(ctx) == cluster.PacketBrokerTenantID {
		logger.Debug("Drop data uplink with Packet Broker tenant")
		return errNoTenant
	}

	var addrMatches []contextualEndDevice
	if err := ns.devices.RangeByAddr(ctx, pld.DevAddr, handleDataUplinkGetPaths[:],
		func(ctx context.Context, dev *ttnpb.EndDevice) bool {
			addrMatches = append(addrMatches, contextualEndDevice{
				Context:   ctx,
				EndDevice: dev,
			})
			return true
		}); err != nil {
		logRegistryRPCError(ctx, err, "Failed to find devices in registry by DevAddr")
		return err
	}

	matched, err := ns.matchAndHandleDataUplink(up, false, addrMatches...)
	if err != nil {
		logger.WithError(err).Debug("Failed to match device")
		return err
	}
	ctx = matched.Context
	logger = log.FromContext(ctx)

	defer func() {
		if err != nil {
			events.Publish(evtDropDataUplink(ctx, matched.Device.EndDeviceIdentifiers, err))
		}
	}()

	ok, err := ns.deduplicateUplink(ctx, up)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ns.deduplicationDone(ctx, up):
	}

	var queuedApplicationUplinks []*ttnpb.ApplicationUp
	var queuedEvents []events.DefinitionDataClosure

	mds, err := ns.uplinkDeduplicator.AccumulatedMetadata(ctx, up)
	if err != nil {
		logger.WithError(err).Error("Failed to merge metadata")
	} else {
		up.RxMetadata = mds
		logger = logger.WithField("metadata_count", len(up.RxMetadata))
		logger.Debug("Merged metadata")
		ctx = log.NewContext(ctx, logger)
		queuedEvents = append(queuedEvents, evtMergeMetadata.BindData(len(up.RxMetadata)))
		registerMergeMetadata(ctx, up)
	}

	for _, f := range matched.DeferredMACHandlers {
		evs, err := f(ctx, matched.Device, up)
		if err != nil {
			logger.WithError(err).Warn("Failed to process MAC command after deduplication")
			break
		}
		matched.QueuedEvents = append(matched.QueuedEvents, evs...)
	}

	stored, storedCtx, err := ns.devices.SetByID(ctx, matched.Device.ApplicationIdentifiers, matched.Device.DeviceID, handleDataUplinkGetPaths[:],
		func(ctx context.Context, stored *ttnpb.EndDevice) (*ttnpb.EndDevice, []string, error) {
			if stored == nil {
				logger.Warn("Device deleted during uplink handling, drop")
				return nil, nil, errOutdatedData.New()
			}

			if !stored.CreatedAt.Equal(matched.Device.CreatedAt) || !stored.UpdatedAt.Equal(matched.Device.UpdatedAt) {
				rematched, err := ns.matchAndHandleDataUplink(up, true, contextualEndDevice{
					Context:   ctx,
					EndDevice: stored,
				})
				if err != nil {
					return nil, nil, errOutdatedData.WithCause(err)
				}
				matched = rematched
			}
			queuedEvents = append(queuedEvents, matched.QueuedEvents...)
			queuedApplicationUplinks = append(queuedApplicationUplinks, matched.QueuedApplicationUplinks...)

			up.DeviceChannelIndex = uint32(matched.ChannelIndex)
			up.Settings.DataRateIndex = matched.DataRateIndex

			stored = matched.Device
			paths := matched.SetPaths

			stored.MACState.RecentUplinks = appendRecentUplink(stored.MACState.RecentUplinks, up, recentUplinkCount)
			paths = ttnpb.AddFields(paths, "mac_state.recent_uplinks")

			stored.RecentUplinks = appendRecentUplink(stored.RecentUplinks, up, recentUplinkCount)
			paths = ttnpb.AddFields(paths, "recent_uplinks")

			paths = ttnpb.AddFields(paths, "recent_adr_uplinks")
			if !pld.FHDR.ADR {
				stored.RecentADRUplinks = nil
				return stored, paths, nil
			}
			stored.RecentADRUplinks = appendRecentUplink(stored.RecentADRUplinks, up, optimalADRUplinkCount)

			if !deviceUseADR(stored, ns.defaultMACSettings) {
				return stored, paths, nil
			}
			if err := adaptDataRate(stored, matched.phy, ns.defaultMACSettings); err != nil {
				return nil, nil, err
			}
			return stored, paths, nil
		})
	if err != nil {
		// TODO: Retry transaction. (https://github.com/TheThingsNetwork/lorawan-stack/issues/33)
		logRegistryRPCError(ctx, err, "Failed to update device in registry")
		return err
	}
	ctx = storedCtx

	if err := ns.updateDataDownlinkTask(ctx, stored, time.Time{}); err != nil {
		logger.WithError(err).Error("Failed to update downlink task queue after data uplink")
	}

	if matched.NbTrans == 1 {
		queuedApplicationUplinks = append(queuedApplicationUplinks, &ttnpb.ApplicationUp{
			EndDeviceIdentifiers: stored.EndDeviceIdentifiers,
			CorrelationIDs:       up.CorrelationIDs,
			Up: &ttnpb.ApplicationUp_UplinkMessage{UplinkMessage: &ttnpb.ApplicationUplink{
				FCnt:         pld.FCnt,
				FPort:        pld.FPort,
				FRMPayload:   pld.FRMPayload,
				RxMetadata:   up.RxMetadata,
				SessionKeyID: stored.Session.SessionKeyID,
				Settings:     up.Settings,
				ReceivedAt:   up.ReceivedAt,
			}},
		})
	}
	if n := len(queuedApplicationUplinks); n > 0 {
		logger := logger.WithField("uplink_count", n)
		logger.Debug("Enqueue application uplinks for sending to Application Server")
		if err := ns.applicationUplinks.Add(ctx, queuedApplicationUplinks...); err != nil {
			logger.WithError(err).Warn("Failed to enqueue application uplinks for sending to Application Server")
		} else if matched.NbTrans == 1 {
			queuedEvents = append(queuedEvents, evtForwardDataUplink.BindData(nil))
			registerForwardDataUplink(ctx, up)
		}
	}
	if n := len(queuedEvents); n > 0 {
		logger := logger.WithField("event_count", n)
		logger.Debug("Publish events")
		for _, ev := range queuedEvents {
			events.Publish(ev(ctx, stored.EndDeviceIdentifiers))
		}
	}
	return nil
}

func (ns *NetworkServer) sendJoinRequest(ctx context.Context, ids ttnpb.EndDeviceIdentifiers, req *ttnpb.JoinRequest) (*ttnpb.JoinResponse, error) {
	logger := log.FromContext(ctx)
	cc, err := ns.GetPeerConn(ctx, ttnpb.ClusterRole_JOIN_SERVER, ids)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.WithError(err).Debug("Join Server peer not found")
		} else {
			logger.WithError(err).Error("Join Server peer connection lookup failed")
		}
	} else {
		resp, err := ttnpb.NewNsJsClient(cc).HandleJoin(ctx, req, ns.WithClusterAuth())
		if err == nil {
			logger.Debug("Join-request accepted by cluster-local Join Server")
			return resp, nil
		}
		logger.WithError(err).Info("Cluster-local Join Server did not accept join-request")
		if !errors.IsNotFound(err) {
			return nil, err
		}
	}
	if ns.interopClient != nil {
		resp, err := ns.interopClient.HandleJoinRequest(ctx, ns.netID, req)
		if err == nil {
			logger.Debug("Join-request accepted by interop Join Server")
			return resp, nil
		}
		logger.WithError(err).Warn("Interop Join Server did not accept join-request")
		if !errors.IsNotFound(err) {
			return nil, err
		}
	}
	return nil, errJoinServerNotFound.New()
}

func (ns *NetworkServer) deduplicationDone(ctx context.Context, up *ttnpb.UplinkMessage) <-chan time.Time {
	return timeAfter(timeUntil(up.ReceivedAt.Add(ns.deduplicationWindow(ctx))))
}

func (ns *NetworkServer) handleJoinRequest(ctx context.Context, up *ttnpb.UplinkMessage) (err error) {
	defer func() {
		if err != nil {
			registerDropJoinRequest(ctx, up, err)
		}
	}()
	pld := up.Payload.GetJoinRequestPayload()

	logger := log.FromContext(ctx).WithFields(log.Fields(
		"dev_eui", pld.DevEUI,
		"join_eui", pld.JoinEUI,
	))
	ctx = log.NewContext(ctx, logger)

	matched, matchedCtx, err := ns.devices.GetByEUI(ctx, pld.JoinEUI, pld.DevEUI,
		[]string{
			"frequency_plan_id",
			"lorawan_phy_version",
			"lorawan_version",
			"mac_settings",
			"session.dev_addr",
			"supports_class_b",
			"supports_class_c",
			"supports_join",
		},
	)
	if err != nil {
		logRegistryRPCError(ctx, err, "Failed to load device from registry by EUIs")
		return err
	}
	ctx = matchedCtx

	defer func() {
		if err != nil {
			events.Publish(evtDropJoinRequest(ctx, matched.EndDeviceIdentifiers, err))
		}
	}()

	ok, err := ns.deduplicateUplink(ctx, up)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	logger = logger.WithField("device_uid", unique.ID(ctx, matched.EndDeviceIdentifiers))

	if !matched.SupportsJoin {
		logger.Warn("ABP device sent a join-request, drop")
		return errABPJoinRequest.New()
	}

	ctx = log.NewContext(ctx, logger)

	devAddr := ns.newDevAddr(ctx, matched)
	for matched.Session != nil && devAddr.Equal(matched.Session.DevAddr) {
		devAddr = ns.newDevAddr(ctx, matched)
	}
	logger = logger.WithField("dev_addr", devAddr)
	ctx = log.NewContext(ctx, logger)

	macState, err := newMACState(matched, ns.FrequencyPlans, ns.defaultMACSettings)
	if err != nil {
		logger.WithError(err).Warn("Failed to reset device's MAC state")
		return err
	}

	fp, phy, err := getDeviceBandVersion(matched, ns.FrequencyPlans)
	if err != nil {
		return err
	}

	req := &ttnpb.JoinRequest{
		Payload:            up.Payload,
		CFList:             frequencyplans.CFList(*fp, matched.LoRaWANPHYVersion),
		CorrelationIDs:     events.CorrelationIDsFromContext(ctx),
		DevAddr:            devAddr,
		NetID:              ns.netID,
		RawPayload:         up.RawPayload,
		RxDelay:            macState.DesiredParameters.Rx1Delay,
		SelectedMACVersion: matched.LoRaWANVersion, // Assume NS version is always higher than the version of the device
		DownlinkSettings: ttnpb.DLSettings{
			Rx1DROffset: macState.DesiredParameters.Rx1DataRateOffset,
			Rx2DR:       macState.DesiredParameters.Rx2DataRateIndex,
			OptNeg:      matched.LoRaWANVersion.Compare(ttnpb.MAC_V1_1) >= 0,
		},
	}

	resp, err := ns.sendJoinRequest(ctx, matched.EndDeviceIdentifiers, req)
	if err != nil {
		return err
	}
	respRecvAt := timeNow()

	ctx = events.ContextWithCorrelationID(ctx, resp.CorrelationIDs...)

	keys := resp.SessionKeys
	if !req.DownlinkSettings.OptNeg {
		keys.NwkSEncKey = keys.FNwkSIntKey
		keys.SNwkSIntKey = keys.FNwkSIntKey
	}

	macState.QueuedJoinAccept = &ttnpb.MACState_JoinAccept{
		Keys:    keys,
		Payload: resp.RawPayload,
		Request: *req,
	}
	macState.RxWindowsAvailable = true

	chIdx, err := searchUplinkChannel(up.Settings.Frequency, macState)
	if err != nil {
		return err
	}
	up.DeviceChannelIndex = uint32(chIdx)

	drIdx, _, ok := phy.FindUplinkDataRate(up.Settings.DataRate)
	if !ok {
		return errDataRateNotFound.New()
	}
	up.Settings.DataRateIndex = drIdx

	events.Publish(evtForwardJoinRequest(ctx, matched.EndDeviceIdentifiers, nil))
	registerForwardJoinRequest(ctx, up)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ns.deduplicationDone(ctx, up):
	}

	var queuedEvents []events.DefinitionDataClosure

	mds, err := ns.uplinkDeduplicator.AccumulatedMetadata(ctx, up)
	if err != nil {
		logger.WithError(err).Error("Failed to merge metadata")
	} else {
		up.RxMetadata = mds
		logger = logger.WithField("metadata_count", len(up.RxMetadata))
		logger.Debug("Merged metadata")
		ctx = log.NewContext(ctx, logger)
		queuedEvents = append(queuedEvents, evtMergeMetadata.BindData(len(up.RxMetadata)))
		registerMergeMetadata(ctx, up)
	}

	var invalidatedQueue []*ttnpb.ApplicationDownlink
	stored, storedCtx, err := ns.devices.SetByID(ctx, matched.EndDeviceIdentifiers.ApplicationIdentifiers, matched.EndDeviceIdentifiers.DeviceID,
		[]string{
			"frequency_plan_id",
			"lorawan_phy_version",
			"pending_session.queued_application_downlinks",
			"recent_uplinks",
			"session.queued_application_downlinks",
		},
		func(ctx context.Context, stored *ttnpb.EndDevice) (*ttnpb.EndDevice, []string, error) {
			if stored == nil {
				logger.Warn("Device deleted during join-request handling, drop")
				return nil, nil, errOutdatedData.New()
			}
			if stored.Session != nil {
				invalidatedQueue = stored.Session.QueuedApplicationDownlinks
			} else {
				invalidatedQueue = stored.GetPendingSession().GetQueuedApplicationDownlinks()
			}
			stored.PendingMACState = macState
			stored.RecentUplinks = appendRecentUplink(stored.RecentUplinks, up, recentUplinkCount)
			return stored, []string{
				"pending_mac_state",
				"recent_uplinks",
			}, nil
		})
	if err != nil {
		// TODO: Retry transaction. (https://github.com/TheThingsNetwork/lorawan-stack/issues/33)
		logRegistryRPCError(ctx, err, "Failed to update device in registry")
		return err
	}
	matched = stored
	ctx = storedCtx

	downAt := up.ReceivedAt.Add(-infrastructureDelay/2 + phy.JoinAcceptDelay1 - req.RxDelay.Duration()/2 - nsScheduleWindow())
	logger.WithField("start_at", downAt).Debug("Add downlink task")
	if err := ns.downlinkTasks.Add(ctx, stored.EndDeviceIdentifiers, downAt, true); err != nil {
		logger.WithError(err).Error("Failed to add downlink task after join-request")
	}
	logger.Debug("Enqueue join-accept for sending to Application Server")
	if err := ns.applicationUplinks.Add(ctx, &ttnpb.ApplicationUp{
		EndDeviceIdentifiers: ttnpb.EndDeviceIdentifiers{
			ApplicationIdentifiers: stored.EndDeviceIdentifiers.ApplicationIdentifiers,
			DeviceID:               stored.EndDeviceIdentifiers.DeviceID,
			DevEUI:                 stored.EndDeviceIdentifiers.DevEUI,
			JoinEUI:                stored.EndDeviceIdentifiers.JoinEUI,
			DevAddr:                &devAddr,
		},
		CorrelationIDs: events.CorrelationIDsFromContext(ctx),
		Up: &ttnpb.ApplicationUp_JoinAccept{JoinAccept: &ttnpb.ApplicationJoinAccept{
			AppSKey:              resp.SessionKeys.AppSKey,
			InvalidatedDownlinks: invalidatedQueue,
			SessionKeyID:         resp.SessionKeys.SessionKeyID,
			ReceivedAt:           respRecvAt,
		}},
	}); err != nil {
		logger.WithError(err).Warn("Failed to enqueue join-accept for sending to Application Server")
	}

	if n := len(queuedEvents); n > 0 {
		logger := logger.WithField("event_count", n)
		logger.Debug("Publish events")
		for _, ev := range queuedEvents {
			events.Publish(ev(ctx, stored.EndDeviceIdentifiers))
		}
	}
	return nil
}

var errRejoinRequest = errors.DefineUnimplemented("rejoin_request", "rejoin-request handling is not implemented")

func (ns *NetworkServer) handleRejoinRequest(ctx context.Context, up *ttnpb.UplinkMessage) (err error) {
	defer func() {
		if err != nil {
			registerDropRejoinRequest(ctx, up, err)
		}
	}()
	// TODO: Implement https://github.com/TheThingsNetwork/lorawan-stack/issues/8
	return errRejoinRequest.New()
}

// HandleUplink is called by the Gateway Server when an uplink message arrives.
func (ns *NetworkServer) HandleUplink(ctx context.Context, up *ttnpb.UplinkMessage) (*pbtypes.Empty, error) {
	if err := clusterauth.Authorized(ctx); err != nil {
		return nil, err
	}

	ctx = events.ContextWithCorrelationID(ctx, append(
		up.CorrelationIDs,
		fmt.Sprintf("ns:uplink:%s", events.NewCorrelationID()),
	)...)
	up.CorrelationIDs = events.CorrelationIDsFromContext(ctx)
	up.ReceivedAt = timeNow().UTC()
	up.Payload = &ttnpb.Message{}
	if err := lorawan.UnmarshalMessage(up.RawPayload, up.Payload); err != nil {
		return nil, errDecodePayload.WithCause(err)
	}

	if up.Payload.Major != ttnpb.Major_LORAWAN_R1 {
		return nil, errUnsupportedLoRaWANVersion.WithAttributes(
			"version", up.Payload.Major,
		)
	}

	logger := log.FromContext(ctx).WithFields(log.Fields(
		"m_type", up.Payload.MType,
		"major", up.Payload.Major,
		"phy_payload_len", len(up.RawPayload),
		"received_at", up.ReceivedAt,
	))
	ctx = log.NewContext(ctx, logger)

	registerReceiveUplink(ctx, up)
	switch up.Payload.MType {
	case ttnpb.MType_CONFIRMED_UP, ttnpb.MType_UNCONFIRMED_UP:
		return ttnpb.Empty, ns.handleDataUplink(ctx, up)
	case ttnpb.MType_JOIN_REQUEST:
		return ttnpb.Empty, ns.handleJoinRequest(ctx, up)
	case ttnpb.MType_REJOIN_REQUEST:
		return ttnpb.Empty, ns.handleRejoinRequest(ctx, up)
	}
	logger.Debug("Unmatched MType")
	return ttnpb.Empty, nil
}

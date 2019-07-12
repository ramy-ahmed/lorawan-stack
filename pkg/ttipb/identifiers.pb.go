// Code generated by protoc-gen-gogo. DO NOT EDIT.
// source: lorawan-stack/api/tti/identifiers.proto

package ttipb

import (
	fmt "fmt"
	io "io"
	math "math"
	reflect "reflect"
	strings "strings"

	_ "github.com/envoyproxy/protoc-gen-validate/validate"
	_ "github.com/gogo/protobuf/gogoproto"
	proto "github.com/gogo/protobuf/proto"
	golang_proto "github.com/golang/protobuf/proto"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = golang_proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.GoGoProtoPackageIsVersion2 // please upgrade the proto package

type TenantIdentifiers struct {
	TenantID             string   `protobuf:"bytes,1,opt,name=tenant_id,json=tenantId,proto3" json:"tenant_id,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *TenantIdentifiers) Reset()      { *m = TenantIdentifiers{} }
func (*TenantIdentifiers) ProtoMessage() {}
func (*TenantIdentifiers) Descriptor() ([]byte, []int) {
	return fileDescriptor_376ea00d21302a82, []int{0}
}
func (m *TenantIdentifiers) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *TenantIdentifiers) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_TenantIdentifiers.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalTo(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (m *TenantIdentifiers) XXX_Merge(src proto.Message) {
	xxx_messageInfo_TenantIdentifiers.Merge(m, src)
}
func (m *TenantIdentifiers) XXX_Size() int {
	return m.Size()
}
func (m *TenantIdentifiers) XXX_DiscardUnknown() {
	xxx_messageInfo_TenantIdentifiers.DiscardUnknown(m)
}

var xxx_messageInfo_TenantIdentifiers proto.InternalMessageInfo

func (m *TenantIdentifiers) GetTenantID() string {
	if m != nil {
		return m.TenantID
	}
	return ""
}

func init() {
	proto.RegisterType((*TenantIdentifiers)(nil), "tti.lorawan.v3.TenantIdentifiers")
	golang_proto.RegisterType((*TenantIdentifiers)(nil), "tti.lorawan.v3.TenantIdentifiers")
}

func init() {
	proto.RegisterFile("lorawan-stack/api/tti/identifiers.proto", fileDescriptor_376ea00d21302a82)
}
func init() {
	golang_proto.RegisterFile("lorawan-stack/api/tti/identifiers.proto", fileDescriptor_376ea00d21302a82)
}

var fileDescriptor_376ea00d21302a82 = []byte{
	// 374 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x64, 0x91, 0x31, 0xac, 0x12, 0x41,
	0x10, 0x86, 0x67, 0xa2, 0x31, 0xef, 0x51, 0x98, 0xf8, 0xaa, 0x17, 0x8a, 0xc1, 0x10, 0x12, 0x31,
	0xe1, 0xee, 0x0c, 0xc4, 0x42, 0x1a, 0x22, 0xb1, 0xb1, 0x25, 0x56, 0x12, 0x34, 0x7b, 0xb0, 0x1c,
	0x1b, 0x60, 0xf7, 0x72, 0x2c, 0x20, 0x1a, 0x13, 0x4a, 0x4a, 0x4b, 0x4b, 0x62, 0x45, 0x49, 0x49,
	0xac, 0x28, 0x29, 0x29, 0xa9, 0x0c, 0xb7, 0xdb, 0x60, 0x47, 0x49, 0x69, 0x3c, 0x30, 0x6a, 0x5e,
	0xf7, 0xff, 0x93, 0x6f, 0xfe, 0x4c, 0xe6, 0x4f, 0x3d, 0xe9, 0xa9, 0x88, 0x8d, 0x99, 0x74, 0x06,
	0x9a, 0x35, 0xbb, 0x1e, 0x0b, 0x85, 0xa7, 0xb5, 0xf0, 0x44, 0x8b, 0x4b, 0x2d, 0xda, 0x82, 0x47,
	0x03, 0x37, 0x8c, 0x94, 0x56, 0x37, 0x0f, 0xb5, 0x16, 0xee, 0x05, 0x76, 0x47, 0xa5, 0xf4, 0xcb,
	0x40, 0xe8, 0xce, 0xd0, 0x77, 0x9b, 0xaa, 0xef, 0x71, 0x39, 0x52, 0x93, 0x30, 0x52, 0x1f, 0x26,
	0x5e, 0x02, 0x37, 0x9d, 0x80, 0x4b, 0x67, 0xc4, 0x7a, 0xa2, 0xc5, 0x34, 0xf7, 0xee, 0x88, 0x73,
	0x64, 0xda, 0xf9, 0x27, 0x22, 0x50, 0x81, 0x3a, 0x2f, 0xfb, 0xc3, 0x76, 0xe2, 0x12, 0x93, 0xa8,
	0x33, 0x9e, 0xed, 0xa7, 0x1e, 0xbd, 0xe1, 0x92, 0x49, 0xfd, 0xfa, 0xef, 0x71, 0x37, 0xb5, 0xd4,
	0xb5, 0x4e, 0x86, 0xef, 0x45, 0xeb, 0x16, 0x1f, 0x63, 0xfe, 0xba, 0xfa, 0xdc, 0xfc, 0xc8, 0x5c,
	0x5d, 0xc8, 0x57, 0xdf, 0x7f, 0xae, 0xef, 0xe5, 0xa2, 0xec, 0x6d, 0xae, 0x48, 0xef, 0xea, 0xcc,
	0xf9, 0xf8, 0xcc, 0x79, 0xd1, 0xc8, 0x57, 0xca, 0x75, 0xa7, 0x51, 0xf9, 0x63, 0x9f, 0x7e, 0x2a,
	0x16, 0x3e, 0xe7, 0x6a, 0x57, 0xfa, 0x12, 0x5e, 0xbe, 0xbf, 0x9a, 0x67, 0xa0, 0xfa, 0x0d, 0x37,
	0x31, 0xe1, 0x36, 0x26, 0xdc, 0xc5, 0x04, 0xfb, 0x98, 0xe0, 0x10, 0x13, 0x1c, 0x63, 0x82, 0x53,
	0x4c, 0x38, 0x35, 0x84, 0x33, 0x43, 0xb0, 0x30, 0x84, 0x4b, 0x43, 0xb0, 0x32, 0x04, 0x6b, 0x43,
	0xb0, 0x31, 0x84, 0x5b, 0x43, 0xb8, 0x33, 0x04, 0x7b, 0x43, 0x78, 0x30, 0x04, 0x47, 0x43, 0x78,
	0x32, 0x04, 0x53, 0x4b, 0x30, 0xb3, 0x84, 0x5f, 0x2c, 0xc1, 0x57, 0x4b, 0x38, 0xb7, 0x04, 0x0b,
	0x4b, 0xb0, 0xb4, 0x84, 0x2b, 0x4b, 0xb8, 0xb6, 0x84, 0x6f, 0x0b, 0x81, 0x72, 0x75, 0x87, 0xeb,
	0x8e, 0x90, 0xc1, 0xc0, 0x95, 0x5c, 0x8f, 0x55, 0xd4, 0xf5, 0xfe, 0x6f, 0x29, 0xec, 0x06, 0xbf,
	0x5b, 0x0a, 0x7d, 0xff, 0x41, 0xf2, 0x9a, 0xd2, 0xaf, 0x00, 0x00, 0x00, 0xff, 0xff, 0xe0, 0x0c,
	0xb5, 0xcb, 0xc7, 0x01, 0x00, 0x00,
}

func (this *TenantIdentifiers) Equal(that interface{}) bool {
	if that == nil {
		return this == nil
	}

	that1, ok := that.(*TenantIdentifiers)
	if !ok {
		that2, ok := that.(TenantIdentifiers)
		if ok {
			that1 = &that2
		} else {
			return false
		}
	}
	if that1 == nil {
		return this == nil
	} else if this == nil {
		return false
	}
	if this.TenantID != that1.TenantID {
		return false
	}
	return true
}
func (m *TenantIdentifiers) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalTo(dAtA)
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *TenantIdentifiers) MarshalTo(dAtA []byte) (int, error) {
	var i int
	_ = i
	var l int
	_ = l
	if len(m.TenantID) > 0 {
		dAtA[i] = 0xa
		i++
		i = encodeVarintIdentifiers(dAtA, i, uint64(len(m.TenantID)))
		i += copy(dAtA[i:], m.TenantID)
	}
	return i, nil
}

func encodeVarintIdentifiers(dAtA []byte, offset int, v uint64) int {
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return offset + 1
}
func (m *TenantIdentifiers) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.TenantID)
	if l > 0 {
		n += 1 + l + sovIdentifiers(uint64(l))
	}
	return n
}

func sovIdentifiers(x uint64) (n int) {
	for {
		n++
		x >>= 7
		if x == 0 {
			break
		}
	}
	return n
}
func sozIdentifiers(x uint64) (n int) {
	return sovIdentifiers((x << 1) ^ uint64((int64(x) >> 63)))
}
func (this *TenantIdentifiers) String() string {
	if this == nil {
		return "nil"
	}
	s := strings.Join([]string{`&TenantIdentifiers{`,
		`TenantID:` + fmt.Sprintf("%v", this.TenantID) + `,`,
		`}`,
	}, "")
	return s
}
func valueToStringIdentifiers(v interface{}) string {
	rv := reflect.ValueOf(v)
	if rv.IsNil() {
		return "nil"
	}
	pv := reflect.Indirect(rv).Interface()
	return fmt.Sprintf("*%v", pv)
}
func (m *TenantIdentifiers) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowIdentifiers
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: TenantIdentifiers: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: TenantIdentifiers: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field TenantID", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowIdentifiers
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				stringLen |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return ErrInvalidLengthIdentifiers
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return ErrInvalidLengthIdentifiers
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.TenantID = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipIdentifiers(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if skippy < 0 {
				return ErrInvalidLengthIdentifiers
			}
			if (iNdEx + skippy) < 0 {
				return ErrInvalidLengthIdentifiers
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func skipIdentifiers(dAtA []byte) (n int, err error) {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return 0, ErrIntOverflowIdentifiers
			}
			if iNdEx >= l {
				return 0, io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		wireType := int(wire & 0x7)
		switch wireType {
		case 0:
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, ErrIntOverflowIdentifiers
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				iNdEx++
				if dAtA[iNdEx-1] < 0x80 {
					break
				}
			}
			return iNdEx, nil
		case 1:
			iNdEx += 8
			return iNdEx, nil
		case 2:
			var length int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, ErrIntOverflowIdentifiers
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				length |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if length < 0 {
				return 0, ErrInvalidLengthIdentifiers
			}
			iNdEx += length
			if iNdEx < 0 {
				return 0, ErrInvalidLengthIdentifiers
			}
			return iNdEx, nil
		case 3:
			for {
				var innerWire uint64
				var start int = iNdEx
				for shift := uint(0); ; shift += 7 {
					if shift >= 64 {
						return 0, ErrIntOverflowIdentifiers
					}
					if iNdEx >= l {
						return 0, io.ErrUnexpectedEOF
					}
					b := dAtA[iNdEx]
					iNdEx++
					innerWire |= (uint64(b) & 0x7F) << shift
					if b < 0x80 {
						break
					}
				}
				innerWireType := int(innerWire & 0x7)
				if innerWireType == 4 {
					break
				}
				next, err := skipIdentifiers(dAtA[start:])
				if err != nil {
					return 0, err
				}
				iNdEx = start + next
				if iNdEx < 0 {
					return 0, ErrInvalidLengthIdentifiers
				}
			}
			return iNdEx, nil
		case 4:
			return iNdEx, nil
		case 5:
			iNdEx += 4
			return iNdEx, nil
		default:
			return 0, fmt.Errorf("proto: illegal wireType %d", wireType)
		}
	}
	panic("unreachable")
}

var (
	ErrInvalidLengthIdentifiers = fmt.Errorf("proto: negative length found during unmarshaling")
	ErrIntOverflowIdentifiers   = fmt.Errorf("proto: integer overflow")
)
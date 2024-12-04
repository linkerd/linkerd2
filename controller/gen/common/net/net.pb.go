// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.35.2
// 	protoc        v3.20.0
// source: common/net.proto

package net

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type IPAddress struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Types that are assignable to Ip:
	//
	//	*IPAddress_Ipv4
	//	*IPAddress_Ipv6
	Ip isIPAddress_Ip `protobuf_oneof:"ip"`
}

func (x *IPAddress) Reset() {
	*x = IPAddress{}
	mi := &file_common_net_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *IPAddress) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*IPAddress) ProtoMessage() {}

func (x *IPAddress) ProtoReflect() protoreflect.Message {
	mi := &file_common_net_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use IPAddress.ProtoReflect.Descriptor instead.
func (*IPAddress) Descriptor() ([]byte, []int) {
	return file_common_net_proto_rawDescGZIP(), []int{0}
}

func (m *IPAddress) GetIp() isIPAddress_Ip {
	if m != nil {
		return m.Ip
	}
	return nil
}

func (x *IPAddress) GetIpv4() uint32 {
	if x, ok := x.GetIp().(*IPAddress_Ipv4); ok {
		return x.Ipv4
	}
	return 0
}

func (x *IPAddress) GetIpv6() *IPv6 {
	if x, ok := x.GetIp().(*IPAddress_Ipv6); ok {
		return x.Ipv6
	}
	return nil
}

type isIPAddress_Ip interface {
	isIPAddress_Ip()
}

type IPAddress_Ipv4 struct {
	Ipv4 uint32 `protobuf:"fixed32,1,opt,name=ipv4,proto3,oneof"`
}

type IPAddress_Ipv6 struct {
	Ipv6 *IPv6 `protobuf:"bytes,2,opt,name=ipv6,proto3,oneof"`
}

func (*IPAddress_Ipv4) isIPAddress_Ip() {}

func (*IPAddress_Ipv6) isIPAddress_Ip() {}

type IPv6 struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	First uint64 `protobuf:"fixed64,1,opt,name=first,proto3" json:"first,omitempty"` // hextets 1-4
	Last  uint64 `protobuf:"fixed64,2,opt,name=last,proto3" json:"last,omitempty"`   // hextets 5-8
}

func (x *IPv6) Reset() {
	*x = IPv6{}
	mi := &file_common_net_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *IPv6) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*IPv6) ProtoMessage() {}

func (x *IPv6) ProtoReflect() protoreflect.Message {
	mi := &file_common_net_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use IPv6.ProtoReflect.Descriptor instead.
func (*IPv6) Descriptor() ([]byte, []int) {
	return file_common_net_proto_rawDescGZIP(), []int{1}
}

func (x *IPv6) GetFirst() uint64 {
	if x != nil {
		return x.First
	}
	return 0
}

func (x *IPv6) GetLast() uint64 {
	if x != nil {
		return x.Last
	}
	return 0
}

type TcpAddress struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Ip   *IPAddress `protobuf:"bytes,1,opt,name=ip,proto3" json:"ip,omitempty"`
	Port uint32     `protobuf:"varint,2,opt,name=port,proto3" json:"port,omitempty"`
}

func (x *TcpAddress) Reset() {
	*x = TcpAddress{}
	mi := &file_common_net_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *TcpAddress) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*TcpAddress) ProtoMessage() {}

func (x *TcpAddress) ProtoReflect() protoreflect.Message {
	mi := &file_common_net_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use TcpAddress.ProtoReflect.Descriptor instead.
func (*TcpAddress) Descriptor() ([]byte, []int) {
	return file_common_net_proto_rawDescGZIP(), []int{2}
}

func (x *TcpAddress) GetIp() *IPAddress {
	if x != nil {
		return x.Ip
	}
	return nil
}

func (x *TcpAddress) GetPort() uint32 {
	if x != nil {
		return x.Port
	}
	return 0
}

var File_common_net_proto protoreflect.FileDescriptor

var file_common_net_proto_rawDesc = []byte{
	0x0a, 0x10, 0x63, 0x6f, 0x6d, 0x6d, 0x6f, 0x6e, 0x2f, 0x6e, 0x65, 0x74, 0x2e, 0x70, 0x72, 0x6f,
	0x74, 0x6f, 0x12, 0x13, 0x6c, 0x69, 0x6e, 0x6b, 0x65, 0x72, 0x64, 0x32, 0x2e, 0x63, 0x6f, 0x6d,
	0x6d, 0x6f, 0x6e, 0x2e, 0x6e, 0x65, 0x74, 0x22, 0x58, 0x0a, 0x09, 0x49, 0x50, 0x41, 0x64, 0x64,
	0x72, 0x65, 0x73, 0x73, 0x12, 0x14, 0x0a, 0x04, 0x69, 0x70, 0x76, 0x34, 0x18, 0x01, 0x20, 0x01,
	0x28, 0x07, 0x48, 0x00, 0x52, 0x04, 0x69, 0x70, 0x76, 0x34, 0x12, 0x2f, 0x0a, 0x04, 0x69, 0x70,
	0x76, 0x36, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x19, 0x2e, 0x6c, 0x69, 0x6e, 0x6b, 0x65,
	0x72, 0x64, 0x32, 0x2e, 0x63, 0x6f, 0x6d, 0x6d, 0x6f, 0x6e, 0x2e, 0x6e, 0x65, 0x74, 0x2e, 0x49,
	0x50, 0x76, 0x36, 0x48, 0x00, 0x52, 0x04, 0x69, 0x70, 0x76, 0x36, 0x42, 0x04, 0x0a, 0x02, 0x69,
	0x70, 0x22, 0x30, 0x0a, 0x04, 0x49, 0x50, 0x76, 0x36, 0x12, 0x14, 0x0a, 0x05, 0x66, 0x69, 0x72,
	0x73, 0x74, 0x18, 0x01, 0x20, 0x01, 0x28, 0x06, 0x52, 0x05, 0x66, 0x69, 0x72, 0x73, 0x74, 0x12,
	0x12, 0x0a, 0x04, 0x6c, 0x61, 0x73, 0x74, 0x18, 0x02, 0x20, 0x01, 0x28, 0x06, 0x52, 0x04, 0x6c,
	0x61, 0x73, 0x74, 0x22, 0x50, 0x0a, 0x0a, 0x54, 0x63, 0x70, 0x41, 0x64, 0x64, 0x72, 0x65, 0x73,
	0x73, 0x12, 0x2e, 0x0a, 0x02, 0x69, 0x70, 0x18, 0x01, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1e, 0x2e,
	0x6c, 0x69, 0x6e, 0x6b, 0x65, 0x72, 0x64, 0x32, 0x2e, 0x63, 0x6f, 0x6d, 0x6d, 0x6f, 0x6e, 0x2e,
	0x6e, 0x65, 0x74, 0x2e, 0x49, 0x50, 0x41, 0x64, 0x64, 0x72, 0x65, 0x73, 0x73, 0x52, 0x02, 0x69,
	0x70, 0x12, 0x12, 0x0a, 0x04, 0x70, 0x6f, 0x72, 0x74, 0x18, 0x02, 0x20, 0x01, 0x28, 0x0d, 0x52,
	0x04, 0x70, 0x6f, 0x72, 0x74, 0x42, 0x37, 0x5a, 0x35, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e,
	0x63, 0x6f, 0x6d, 0x2f, 0x6c, 0x69, 0x6e, 0x6b, 0x65, 0x72, 0x64, 0x2f, 0x6c, 0x69, 0x6e, 0x6b,
	0x65, 0x72, 0x64, 0x32, 0x2f, 0x63, 0x6f, 0x6e, 0x74, 0x72, 0x6f, 0x6c, 0x6c, 0x65, 0x72, 0x2f,
	0x67, 0x65, 0x6e, 0x2f, 0x63, 0x6f, 0x6d, 0x6d, 0x6f, 0x6e, 0x2f, 0x6e, 0x65, 0x74, 0x62, 0x06,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_common_net_proto_rawDescOnce sync.Once
	file_common_net_proto_rawDescData = file_common_net_proto_rawDesc
)

func file_common_net_proto_rawDescGZIP() []byte {
	file_common_net_proto_rawDescOnce.Do(func() {
		file_common_net_proto_rawDescData = protoimpl.X.CompressGZIP(file_common_net_proto_rawDescData)
	})
	return file_common_net_proto_rawDescData
}

var file_common_net_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_common_net_proto_goTypes = []any{
	(*IPAddress)(nil),  // 0: linkerd2.common.net.IPAddress
	(*IPv6)(nil),       // 1: linkerd2.common.net.IPv6
	(*TcpAddress)(nil), // 2: linkerd2.common.net.TcpAddress
}
var file_common_net_proto_depIdxs = []int32{
	1, // 0: linkerd2.common.net.IPAddress.ipv6:type_name -> linkerd2.common.net.IPv6
	0, // 1: linkerd2.common.net.TcpAddress.ip:type_name -> linkerd2.common.net.IPAddress
	2, // [2:2] is the sub-list for method output_type
	2, // [2:2] is the sub-list for method input_type
	2, // [2:2] is the sub-list for extension type_name
	2, // [2:2] is the sub-list for extension extendee
	0, // [0:2] is the sub-list for field type_name
}

func init() { file_common_net_proto_init() }
func file_common_net_proto_init() {
	if File_common_net_proto != nil {
		return
	}
	file_common_net_proto_msgTypes[0].OneofWrappers = []any{
		(*IPAddress_Ipv4)(nil),
		(*IPAddress_Ipv6)(nil),
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_common_net_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_common_net_proto_goTypes,
		DependencyIndexes: file_common_net_proto_depIdxs,
		MessageInfos:      file_common_net_proto_msgTypes,
	}.Build()
	File_common_net_proto = out.File
	file_common_net_proto_rawDesc = nil
	file_common_net_proto_goTypes = nil
	file_common_net_proto_depIdxs = nil
}

//go:build !tinygo

package dns

var zoneCreate = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("dns.zoneCreate: requires TinyGo")
}
var zoneGet = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("dns.zoneGet: requires TinyGo")
}
var zoneList = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("dns.zoneList: requires TinyGo")
}
var zoneDelete = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("dns.zoneDelete: requires TinyGo")
}
var recordCreate = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("dns.recordCreate: requires TinyGo")
}
var recordList = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("dns.recordList: requires TinyGo")
}
var recordDelete = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("dns.recordDelete: requires TinyGo")
}

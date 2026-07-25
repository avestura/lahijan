//go:build !tinygo

package storage

var bucketCreate = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("storage.bucketCreate: requires TinyGo")
}
var bucketGet = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("storage.bucketGet: requires TinyGo")
}
var bucketList = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("storage.bucketList: requires TinyGo")
}
var bucketDelete = func(argsPtr, argsLen, bufPtr, bufCap uint32) int32 {
	panic("storage.bucketDelete: requires TinyGo")
}

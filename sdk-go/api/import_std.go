//go:build !tinygo

package api

var apiRegisterHandler = func(methodPtr, methodLen, pathPtr, pathLen, handlerPtr, handlerLen uint32) int32 {
	panic("api.apiRegisterHandler: requires TinyGo")
}

var apiUnregisterHandler = func(methodPtr, methodLen, pathPtr, pathLen uint32) int32 {
	panic("api.apiUnregisterHandler: requires TinyGo")
}

//go:build !tinygo

package network

var networkHTTPRequest = func(
	methodPtr, methodLen, urlPtr, urlLen,
	reqHeadersPtr, reqHeadersLen, reqBodyPtr, reqBodyLen,
	respHdrBufPtr, respHdrBufCap,
	respBodyBufPtr, respBodyBufCap uint32,
) int32 {
	panic("network.networkHTTPRequest: requires TinyGo")
}

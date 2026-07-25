//go:build tinygo

package network

// networkHTTPRequest is the raw WASM import for lahijan_network.http_request.
// The 12-param ABI (ADR-0038):
//
//	http_request(
//	    method_ptr, method_len,           // request method
//	    url_ptr, url_len,                 // request URL
//	    reqHeaders_ptr, reqHeaders_len,   // request headers (JSON map)
//	    reqBody_ptr, reqBody_len,         // request body
//	    respHdrBuf_ptr, respHdrBuf_cap,   // response header buffer (host writes LE-len + JSON)
//	    respBodyBuf_ptr, respBodyBuf_cap, // response body buffer (host writes LE-len + bytes)
//	) -> int32                            // HTTP status (>0) or StatusXxx (<0)
//
//go:wasm-import lahijan_network http_request
func networkHTTPRequest(
	methodPtr, methodLen, urlPtr, urlLen,
	reqHeadersPtr, reqHeadersLen, reqBodyPtr, reqBodyLen,
	respHdrBufPtr, respHdrBufCap,
	respBodyBufPtr, respBodyBufCap uint32,
) int32

//go:build tinygo

package events

//go:wasm-import lahijan_events emit
func eventsEmit(topicPtr, topicLen, payloadPtr, payloadLen uint32) int32

//go:wasm-import lahijan_events subscribe
func eventsSubscribe(topicPtr, topicLen, handlerPtr, handlerLen uint32) int32

//go:wasm-import lahijan_events unsubscribe
func eventsUnsubscribe(topicPtr, topicLen, handlerPtr, handlerLen uint32) int32

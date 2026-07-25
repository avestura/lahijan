//go:build !tinygo

package events

var eventsEmit = func(topicPtr, topicLen, payloadPtr, payloadLen uint32) int32 {
	panic("events.eventsEmit: requires TinyGo")
}

var eventsSubscribe = func(topicPtr, topicLen, handlerPtr, handlerLen uint32) int32 {
	panic("events.eventsSubscribe: requires TinyGo")
}

var eventsUnsubscribe = func(topicPtr, topicLen, handlerPtr, handlerLen uint32) int32 {
	panic("events.eventsUnsubscribe: requires TinyGo")
}

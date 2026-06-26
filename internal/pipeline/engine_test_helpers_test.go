package pipeline

import "github.com/igormaneschy/aurelia/internal/engine"

func testResultEvent(content string, fields ...func(*engine.Event)) engine.Event {
	ev := engine.Event{
		Type:    engine.EventTypeDone,
		RawType: "result",
		Content: content,
	}
	for _, fn := range fields {
		fn(&ev)
	}
	return ev
}

func testErrorEvent(message string) engine.Event {
	return engine.Event{
		Type:    engine.EventTypeError,
		RawType: "error",
		Message: message,
	}
}
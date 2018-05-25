package inboxes

import "testing"

func TestTopicMatch(t *testing.T) {
	entry := "foo.bar.baz:123"

	matches := []string{
		"foo.bar.baz:123",
		"foo.bar.baz",
		"foo.bar",
		"foo",
		"*",
	}

	exceptions := []string{
		"foo.bar.baz:1234",
		"foo.bar.baz:12",
		"foo.bar.b",
		"noo",
		"",
	}

	for _, text := range matches {
		if !topicMatch(entry, text) {
			t.Errorf("expected match of %s to %s", entry, text)
		}
	}

	for _, text := range exceptions {
		if topicMatch(entry, text) {
			t.Errorf("expected non match of %s to %s", entry, text)
		}
	}
}

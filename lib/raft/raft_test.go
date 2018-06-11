package raft

import (
	"reflect"
	"testing"

	"github.com/norganna/logeric"
	"github.com/sirupsen/logrus/hooks/test"
)

func TestStore_Apply(t *testing.T) {
	log, _ := test.NewNullLogger()
	logger, _ := logeric.New(log)

	tests := []struct {
		name string
		cc   []Command
		want map[string]string
	}{
		{
			"set",
			[]Command{
				{Operation: "set", Key: "a", Value: "b"},                             // {a:b}
				{Operation: "set", Key: "b", Value: "b"},                             // {a:b, b:b}
				{Operation: "set", Key: "c", Value: "b"},                             // {a:b, b:b, c:b}
				{Operation: "set", Key: "d", Value: "c"},                             // {a:b, b:b, c:b d:c}
				{Operation: "set", Key: "b", Value: "c", Compare: "eq", Versus: "b"}, // {a:b, b:c, c:b, d:c}
				{Operation: "set", Key: "a", Value: "d", Compare: "eq", Versus: "c"}, // {a:b, b:c, c:b, d:c}
				{Operation: "delete", Key: "c"},                                      // {a:b, b:c, d:c}
			},
			map[string]string{
				"a": "b",
				"b": "c",
				"d": "c",
			},
		},
		{
			"inc",
			[]Command{
				{Operation: "set", Key: "a", Value: "5"},       // = 5
				{Operation: "increment", Key: "a"},             // +1 = 6
				{Operation: "increment", Key: "a", Value: "2"}, // +2 = 8
				{Operation: "increment", Key: "a", Value: "0"}, // +0 = 8
				{Operation: "decrement", Key: "a"},             // -1 = 7
				{Operation: "decrement", Key: "a", Value: "5"}, // -5 = 2
				{Operation: "decrement", Key: "a", Value: "0"}, // -0 = 2
			},
			map[string]string{"a": "2"},
		},
		{
			"tag",
			[]Command{
				{Operation: "tag", Key: "a", Value: "a"},   // start {a}
				{Operation: "tag", Key: "a", Value: "e"},   // end {a,e}
				{Operation: "tag", Key: "a", Value: "c"},   // middle {a,c,e}
				{Operation: "tag", Key: "a", Value: "b"},   // middle {a,b,c,e}
				{Operation: "tag", Key: "a", Value: "d"},   // middle {a,b,c,d,e}
				{Operation: "untag", Key: "a", Value: "b"}, // middle {a,c,d,e}
				{Operation: "untag", Key: "a", Value: "a"}, // start {c,d,e}
				{Operation: "untag", Key: "a", Value: "e"}, // end {c,d}
			},
			map[string]string{"a": "c\td"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Store{
				m:      map[string]string{},
				logger: logger,
			}
			for _, c := range tt.cc {
				s.applyCommand(c)
			}
			if !reflect.DeepEqual(s.m, tt.want) {
				t.Errorf("Store.Apply() %s: got %v, want %v", tt.name, s.m, tt.want)
			}
		})
	}
}

package chats

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// ruleParam digs the parameter of one validate rule out of a struct tag.
func ruleParam(t *testing.T, payload any, field, rule string) string {
	t.Helper()
	sf, ok := reflect.TypeOf(payload).FieldByName(field)
	if !ok {
		t.Fatalf("%T has no field %s", payload, field)
	}
	for _, r := range strings.Split(sf.Tag.Get("validate"), ",") {
		name, param, found := strings.Cut(r, "=")
		if name == rule {
			if !found {
				t.Fatalf("rule %q on %T.%s has no parameter", rule, payload, field)
			}
			return param
		}
	}
	t.Fatalf("no %q rule on %T.%s (tag: %q)", rule, payload, field, sf.Tag.Get("validate"))
	return ""
}

// Struct tags cannot reference Go constants, so this test is the only thing
// keeping the HTTP-boundary limits in step with the package constants.
func TestTagsMatchPackageLimits(t *testing.T) {
	t.Run("chat name max", func(t *testing.T) {
		got, err := strconv.Atoi(ruleParam(t, createRequest{}, "Name", "max"))
		if err != nil {
			t.Fatalf("parse max: %v", err)
		}
		if got != maxNameLen {
			t.Errorf("tag max=%d, maxNameLen=%d", got, maxNameLen)
		}
	})

	t.Run("message text max", func(t *testing.T) {
		got, err := strconv.Atoi(ruleParam(t, sendMessageRequest{}, "Text", "max"))
		if err != nil {
			t.Fatalf("parse max: %v", err)
		}
		if got != maxMessageLen {
			t.Errorf("tag max=%d, maxMessageLen=%d", got, maxMessageLen)
		}
	})

	t.Run("chat type oneof lists every type", func(t *testing.T) {
		got := strings.Fields(ruleParam(t, createRequest{}, "Type", "oneof"))
		want := []string{TypePublic, TypeGroup}
		if len(got) != len(want) {
			t.Fatalf("oneof = %v, want %v", got, want)
		}
		for _, typ := range want {
			if !slices.Contains(got, typ) {
				t.Errorf("oneof = %v, missing type %q", got, typ)
			}
		}
	})
}

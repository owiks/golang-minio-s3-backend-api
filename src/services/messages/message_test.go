package messages

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDescriptionsAreImmutableAndRequireProjection(t *testing.T) {
	args := []Argument{String("Name", "original"), Text("Cause", UploadSizeMismatch)}
	ref := InternalError.With(args...)
	before := Identity(ref)
	args[0] = String("Name", "mutated")
	d := Describe(ref)
	d.Arguments[0] = String("Name", "detached")
	if Identity(ref) != before {
		t.Fatal("argument alias")
	}
	for _, value := range []Ref{InternalError, ref, Literal("opaque")} {
		if _, err := json.Marshal(value); !errors.Is(err, ErrUnrendered) {
			t.Fatal("unrendered message escaped", err)
		}
	}
	ids := AllIDs()
	first := ids[0]
	ids[0] = "changed"
	if AllIDs()[0] != first {
		t.Fatal("mutable catalog keys")
	}
}

package webdav

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
)

type copyMoveOperationsStub struct {
	calls      []string
	deleteErr  error
	moveErr    error
	renameErr  error
	moveIsCopy bool
}

func (s *copyMoveOperationsStub) Delete(context.Context, []*fs.URI, ...fs.Option) error {
	s.calls = append(s.calls, "delete")
	return s.deleteErr
}

func (s *copyMoveOperationsStub) MoveOrCopy(_ context.Context, _ []*fs.URI, _ *fs.URI, isCopy bool) error {
	s.calls = append(s.calls, "move")
	s.moveIsCopy = isCopy
	return s.moveErr
}

func (s *copyMoveOperationsStub) Rename(context.Context, *fs.URI, string) (fs.File, error) {
	s.calls = append(s.calls, "rename")
	return nil, s.renameErr
}

func TestParseOverwrite(t *testing.T) {
	tests := []struct {
		value string
		want  bool
		err   bool
	}{
		{value: "", want: true},
		{value: "T", want: true},
		{value: "F", want: false},
		{value: "true", err: true},
	}

	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			got, err := parseOverwrite(test.value)
			if (err != nil) != test.err {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != test.want {
				t.Fatalf("unexpected overwrite value: %v", got)
			}
		})
	}
}

func TestPerformCopyMove(t *testing.T) {
	src := mustWebDAVTestURI(t, "cloudreve://my/source.temp")
	dst := mustWebDAVTestURI(t, "cloudreve://my/source.txt")
	dstFolder := dst.DirUri()

	t.Run("overwrite disabled", func(t *testing.T) {
		operations := &copyMoveOperationsStub{}
		status, err := performCopyMove(context.Background(), operations, src, dst, dstFolder, false, false, true)
		if status != http.StatusPreconditionFailed || !errors.Is(err, errDestinationExists) {
			t.Fatalf("unexpected result: status=%d err=%v", status, err)
		}
		if len(operations.calls) != 0 {
			t.Fatalf("unexpected operations: %v", operations.calls)
		}
	})

	t.Run("overwrite existing move", func(t *testing.T) {
		operations := &copyMoveOperationsStub{}
		status, err := performCopyMove(context.Background(), operations, src, dst, dstFolder, false, true, true)
		if err != nil || status != http.StatusNoContent {
			t.Fatalf("unexpected result: status=%d err=%v", status, err)
		}
		if got := operations.calls; len(got) != 3 || got[0] != "delete" || got[1] != "move" || got[2] != "rename" {
			t.Fatalf("unexpected operation order: %v", got)
		}
		if operations.moveIsCopy {
			t.Fatal("move was executed as copy")
		}
	})

	t.Run("new copy", func(t *testing.T) {
		operations := &copyMoveOperationsStub{}
		status, err := performCopyMove(context.Background(), operations, src, dst, dstFolder, true, true, false)
		if err != nil || status != http.StatusCreated {
			t.Fatalf("unexpected result: status=%d err=%v", status, err)
		}
		if got := operations.calls; len(got) != 2 || got[0] != "move" || got[1] != "rename" {
			t.Fatalf("unexpected operation order: %v", got)
		}
		if !operations.moveIsCopy {
			t.Fatal("copy was executed as move")
		}
	})
}

func mustWebDAVTestURI(t *testing.T, raw string) *fs.URI {
	t.Helper()
	uri, err := fs.NewUriFromString(raw)
	if err != nil {
		t.Fatal(err)
	}
	return uri
}

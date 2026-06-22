package grpc

import (
	"errors"
	"testing"

	"github.com/gcclinux/tergum/internal/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"pgregory.net/rapid"
)

// TestProperty_ErrorCodeMappingConsistency verifies that the same error type always
// maps to the same CLI exit code and gRPC status code, regardless of the error message.
//
// **Validates: Requirements 13.2, 22.4**

func TestProperty_ConfigErrorMapping(t *testing.T) {
	// Property 12: Error Code Mapping Consistency
	// For random error messages, ConfigError always maps to codes.InvalidArgument and exit code 2.
	rapid.Check(t, func(t *rapid.T) {
		msg := rapid.String().Draw(t, "message")
		err := &model.ConfigError{Message: msg}

		// Verify gRPC code mapping
		grpcErr := MapError(err)
		st, ok := status.FromError(grpcErr)
		if !ok {
			t.Fatal("MapError did not return a gRPC status error")
		}
		if st.Code() != codes.InvalidArgument {
			t.Fatalf("ConfigError mapped to %v, want codes.InvalidArgument", st.Code())
		}

		// Verify CLI exit code mapping
		exitCode := model.GetExitCode(err)
		if exitCode != 2 {
			t.Fatalf("ConfigError exit code = %d, want 2", exitCode)
		}
	})
}

func TestProperty_ConnectionErrorMapping(t *testing.T) {
	// Property 12: Error Code Mapping Consistency
	// For random error messages, ConnectionError always maps to codes.Unavailable and exit code 3.
	rapid.Check(t, func(t *rapid.T) {
		msg := rapid.String().Draw(t, "message")
		err := &model.ConnectionError{Message: msg}

		// Verify gRPC code mapping
		grpcErr := MapError(err)
		st, ok := status.FromError(grpcErr)
		if !ok {
			t.Fatal("MapError did not return a gRPC status error")
		}
		if st.Code() != codes.Unavailable {
			t.Fatalf("ConnectionError mapped to %v, want codes.Unavailable", st.Code())
		}

		// Verify CLI exit code mapping
		exitCode := model.GetExitCode(err)
		if exitCode != 3 {
			t.Fatalf("ConnectionError exit code = %d, want 3", exitCode)
		}
	})
}

func TestProperty_AuthErrorMapping(t *testing.T) {
	// Property 12: Error Code Mapping Consistency
	// For random error messages, AuthError always maps to codes.Unauthenticated and exit code 4.
	rapid.Check(t, func(t *rapid.T) {
		msg := rapid.String().Draw(t, "message")
		err := &model.AuthError{Message: msg}

		// Verify gRPC code mapping
		grpcErr := MapError(err)
		st, ok := status.FromError(grpcErr)
		if !ok {
			t.Fatal("MapError did not return a gRPC status error")
		}
		if st.Code() != codes.Unauthenticated {
			t.Fatalf("AuthError mapped to %v, want codes.Unauthenticated", st.Code())
		}

		// Verify CLI exit code mapping
		exitCode := model.GetExitCode(err)
		if exitCode != 4 {
			t.Fatalf("AuthError exit code = %d, want 4", exitCode)
		}
	})
}

func TestProperty_StorageErrorMapping(t *testing.T) {
	// Property 12: Error Code Mapping Consistency
	// For random error messages, StorageError always maps to codes.ResourceExhausted and exit code 5.
	rapid.Check(t, func(t *rapid.T) {
		msg := rapid.String().Draw(t, "message")
		err := &model.StorageError{Message: msg}

		// Verify gRPC code mapping
		grpcErr := MapError(err)
		st, ok := status.FromError(grpcErr)
		if !ok {
			t.Fatal("MapError did not return a gRPC status error")
		}
		if st.Code() != codes.ResourceExhausted {
			t.Fatalf("StorageError mapped to %v, want codes.ResourceExhausted", st.Code())
		}

		// Verify CLI exit code mapping
		exitCode := model.GetExitCode(err)
		if exitCode != 5 {
			t.Fatalf("StorageError exit code = %d, want 5", exitCode)
		}
	})
}

func TestProperty_StoppedErrorMapping(t *testing.T) {
	// Property 12: Error Code Mapping Consistency
	// For random error messages, StoppedError always maps to codes.Canceled and exit code 10.
	rapid.Check(t, func(t *rapid.T) {
		msg := rapid.String().Draw(t, "message")
		err := &model.StoppedError{Message: msg}

		// Verify gRPC code mapping
		grpcErr := MapError(err)
		st, ok := status.FromError(grpcErr)
		if !ok {
			t.Fatal("MapError did not return a gRPC status error")
		}
		if st.Code() != codes.Canceled {
			t.Fatalf("StoppedError mapped to %v, want codes.Canceled", st.Code())
		}

		// Verify CLI exit code mapping
		exitCode := model.GetExitCode(err)
		if exitCode != 10 {
			t.Fatalf("StoppedError exit code = %d, want 10", exitCode)
		}
	})
}

func TestProperty_BackupFailedErrorMapping(t *testing.T) {
	// Property 12: Error Code Mapping Consistency
	// For random error messages, BackupFailedError always maps to codes.Internal and exit code 11.
	rapid.Check(t, func(t *rapid.T) {
		msg := rapid.String().Draw(t, "message")
		err := &model.BackupFailedError{Message: msg}

		// Verify gRPC code mapping
		grpcErr := MapError(err)
		st, ok := status.FromError(grpcErr)
		if !ok {
			t.Fatal("MapError did not return a gRPC status error")
		}
		if st.Code() != codes.Internal {
			t.Fatalf("BackupFailedError mapped to %v, want codes.Internal", st.Code())
		}

		// Verify CLI exit code mapping
		exitCode := model.GetExitCode(err)
		if exitCode != 11 {
			t.Fatalf("BackupFailedError exit code = %d, want 11", exitCode)
		}
	})
}

func TestProperty_NilErrorMapping(t *testing.T) {
	// Property 12: Error Code Mapping Consistency
	// For nil errors, MapError returns nil.
	result := MapError(nil)
	if result != nil {
		t.Fatalf("MapError(nil) = %v, want nil", result)
	}
}

func TestProperty_GenericErrorMapping(t *testing.T) {
	// Property 12: Error Code Mapping Consistency
	// For generic errors, MapError returns codes.Internal.
	rapid.Check(t, func(t *rapid.T) {
		msg := rapid.String().Draw(t, "message")
		err := errors.New(msg)

		grpcErr := MapError(err)
		st, ok := status.FromError(grpcErr)
		if !ok {
			t.Fatal("MapError did not return a gRPC status error")
		}
		if st.Code() != codes.Internal {
			t.Fatalf("generic error mapped to %v, want codes.Internal", st.Code())
		}
	})
}

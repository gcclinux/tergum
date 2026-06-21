package webui

import (
	"testing"
)

func TestValidatePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "empty string", path: "", wantErr: true},
		{name: "single space", path: " ", wantErr: true},
		{name: "multiple spaces", path: "   ", wantErr: true},
		{name: "tab", path: "\t", wantErr: true},
		{name: "newline", path: "\n", wantErr: true},
		{name: "mixed whitespace", path: " \t\n\r ", wantErr: true},
		{name: "valid path", path: "/home/user/docs", wantErr: false},
		{name: "path with leading space", path: " /home/user", wantErr: false},
		{name: "path with trailing space", path: "/home/user ", wantErr: false},
		{name: "relative path", path: "docs/folder", wantErr: false},
		{name: "dot path", path: ".", wantErr: false},
		{name: "single char", path: "a", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if err != nil && err != ErrPathEmpty {
				t.Errorf("ValidatePath(%q) returned unexpected error: %v", tt.path, err)
			}
		})
	}
}

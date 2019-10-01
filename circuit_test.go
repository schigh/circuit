package circuit

import (
	"reflect"
	"testing"
)

func TestState_MarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		s       State
		want    []byte
		wantErr bool
	}{
		{
			name:    "open",
			s:       Open,
			want:    []byte(`"open"`),
			wantErr: false,
		},
		{
			name:    "throttled",
			s:       Throttled,
			want:    []byte(`"throttled"`),
			wantErr: false,
		},
		{
			name:    "closed",
			s:       Closed,
			want:    []byte(`"closed"`),
			wantErr: false,
		},
		{
			name:    "unknown",
			s:       State(100),
			want:    []byte(`"unknown"`),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.s.MarshalJSON()
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MarshalJSON() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		name string
		s    State
		want string
	}{
		{
			name: "open",
			s:    Open,
			want: "open",
		},
		{
			name: "throttled",
			s:    Throttled,
			want: "throttled",
		},
		{
			name: "closed",
			s:    Closed,
			want: "closed",
		},
		{
			name: "unknown",
			s:    State(100),
			want: "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

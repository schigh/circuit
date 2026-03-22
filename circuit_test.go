package circuit

import (
	"encoding/json"
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

func TestState_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    State
		wantErr bool
	}{
		{name: "closed", input: []byte(`"closed"`), want: Closed},
		{name: "throttled", input: []byte(`"throttled"`), want: Throttled},
		{name: "open", input: []byte(`"open"`), want: Open},
		{name: "unknown", input: []byte(`"bogus"`), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s State
			err := json.Unmarshal(tt.input, &s)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && s != tt.want {
				t.Errorf("UnmarshalJSON() got = %v, want %v", s, tt.want)
			}
		})
	}
}

func TestState_RoundTrip(t *testing.T) {
	for _, s := range []State{Closed, Throttled, Open} {
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("Marshal(%v) error: %v", s, err)
		}
		var got State
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal(%v) error: %v", string(data), err)
		}
		if got != s {
			t.Fatalf("round-trip failed: got %v, want %v", got, s)
		}
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

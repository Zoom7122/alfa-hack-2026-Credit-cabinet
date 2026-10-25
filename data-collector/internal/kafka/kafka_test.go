package kafka

import (
	"slices"
	"testing"
)

func TestBrokers(t *testing.T) {
	cases := map[string][]string{
		"kafka:9092":                      {"kafka:9092"},
		" kafka:9092 , kafka-2:9092 ,":    {"kafka:9092", "kafka-2:9092"},
		"":                                nil,
		"localhost:9094,,localhost:9095 ": {"localhost:9094", "localhost:9095"},
	}
	for in, want := range cases {
		if got := Brokers(in); !slices.Equal(got, want) {
			t.Errorf("Brokers(%q) = %v, want %v", in, got, want)
		}
	}
}

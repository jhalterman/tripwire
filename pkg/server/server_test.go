package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStage_ServiceTime(t *testing.T) {
	tests := []struct {
		name        string
		stage       Stage
		weight      int
		expected    time.Duration
		expectedErr error
	}{
		{
			name: "single service time match",
			stage: Stage{
				ServiceTimes: []*WeightedServiceTime{
					{Weight: 10, ServiceTime: 50 * time.Millisecond},
				},
			},
			weight:      5,
			expected:    50 * time.Millisecond,
			expectedErr: nil,
		},
		{
			name: "single service time match with default weight",
			stage: Stage{
				ServiceTimes: []*WeightedServiceTime{
					{Weight: 10, ServiceTime: 50 * time.Millisecond},
				},
			},
			weight:      0,
			expected:    50 * time.Millisecond,
			expectedErr: nil,
		},
		{
			name: "multiple satencies match first",
			stage: Stage{
				ServiceTimes: []*WeightedServiceTime{
					{Weight: 10, ServiceTime: 50 * time.Millisecond},
					{Weight: 20, ServiceTime: 100 * time.Millisecond},
				},
			},
			weight:      8,
			expected:    50 * time.Millisecond,
			expectedErr: nil,
		},
		{
			name: "multiple service times match second",
			stage: Stage{
				ServiceTimes: []*WeightedServiceTime{
					{Weight: 10, ServiceTime: 50 * time.Millisecond},
					{Weight: 20, ServiceTime: 100 * time.Millisecond},
				},
			},
			weight:      15,
			expected:    100 * time.Millisecond,
			expectedErr: nil,
		},
		{
			name: "multiple service times  with default weight",
			stage: Stage{
				ServiceTimes: []*WeightedServiceTime{
					{Weight: 10, ServiceTime: 50 * time.Millisecond},
					{Weight: 20, ServiceTime: 100 * time.Millisecond},
				},
			},
			weight:      0,
			expected:    50 * time.Millisecond,
			expectedErr: nil,
		},
		{
			name: "no matching service time",
			stage: Stage{
				ServiceTimes: []*WeightedServiceTime{
					{Weight: 10, ServiceTime: 50 * time.Millisecond},
				},
			},
			weight:      20,
			expected:    0,
			expectedErr: fmt.Errorf("failed to compute service time"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.stage.serviceTime(tt.weight)
			assert.Equal(t, tt.expected, result)
			if tt.expectedErr != nil {
				assert.EqualError(t, err, tt.expectedErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

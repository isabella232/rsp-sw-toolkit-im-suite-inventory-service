package tagprocessor

import (
	"fmt"
	"math"
	"testing"
)

var (
	// epsilon is used to compare floating point numbers to each other
	epsilon = math.Nextafter(1.0, 2.0) - 1.0
)

func assertBufferSize(t *testing.T, buff *CircularBuffer, expectedSize int) {
	if buff.getN() != expectedSize {
		t.Errorf("expected buffer size of %d, but was %d", buff.getN(), expectedSize)
	}
}

func TestSensorGetAntennaAlias(t *testing.T) {
	tests := []struct {
		deviceId string
		antennaId int
		expected string
	}{
		{
			deviceId: "RSP-3F7DAC",
			antennaId: 0,
			expected: "RSP-3F7DAC-0",
		},
		{
			deviceId: "RSP-150000",
			antennaId: 10,
			expected: "RSP-150000-10",
		},
		{
			deviceId: "RSP-999999",
			antennaId: 3,
			expected: "RSP-999999-3",
		},
	}

	for _, test := range tests {
		t.Run(test.expected, func(t *testing.T) {
			sensor := NewRfidSensor(test.deviceId)
			alias := sensor.getAntennaAlias(test.antennaId)
			if alias != test.expected {
				t.Errorf("Expected alias of %s, but got %s", test.expected, alias)
			}
		})
	}
}

func TestCircularBufferAddValue(t *testing.T) {
	tests := []int{1, 5, 10, 20, 100, 999}

	for _, window := range tests {
		t.Run(fmt.Sprintf("WindowOf%d", window), func(t *testing.T) {
			buff := NewCircularBuffer(window)

			assertBufferSize(t, buff, 0)
			// fill up the buffer
			for i := 0; i < window; i++ {
				buff.addValue(float64(i))
			}
			assertBufferSize(t, buff, window)

			// attempt to overflow
			for i := 0; i < window*5; i++ {
				buff.addValue(float64(i))
				// make sure does not overflow
				assertBufferSize(t, buff, window)
			}
		})
	}
}

func TestCircularBufferGetMean(t *testing.T) {
	tests := []struct {
		name     string
		window   int
		data     []float64
		expected float64
	}{
		{
			name:     "Basic",
			window:   10,
			data:     []float64{1, 2, 3, 4, 5},
			expected: 3,
		},
		{
			name:     "Basic 2",
			window:   10,
			data:     []float64{10, 20},
			expected: 15,
		},
		{
			name:     "Circular Overflow",
			window:   2,
			data:     []float64{5, 20, 20},
			expected: 20,
		},
		{
			name:     "Circular Overflow 2",
			window:   3,
			data:     []float64{5, 5, 5, 5, 5, 5, 5, 5, 6, 100},
			expected: 37,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buff := NewCircularBuffer(test.window)
			for _, val := range test.data {
				buff.addValue(val)
			}

			mean := buff.getMean()
			if math.Abs(mean-test.expected) > epsilon {
				t.Errorf("expected mean of %v, but got %v", test.expected, mean)
			}
		})
	}
}

package geo

import "testing"

var square = [][]float64{{37.7749, -122.4194}, {37.7849, -122.4194}, {37.7849, -122.4094}, {37.7749, -122.4094}, {37.7749, -122.4194}}

func TestValidatePolygon(t *testing.T) {
	tests := []struct {
		name    string
		coords  [][]float64
		wantErr bool
	}{
		{"valid square", square, false},
		{"too few points", square[:3], true},
		{"not closed", [][]float64{{0, 0}, {0, 1}, {1, 1}, {1, 0}}, true},
		{"bad latitude", [][]float64{{91, 0}, {0, 1}, {1, 1}, {91, 0}}, true},
		{"bad longitude", [][]float64{{0, 181}, {0, 1}, {1, 1}, {0, 181}}, true},
		{"malformed pair", [][]float64{{0, 0}, {0}, {1, 1}, {0, 0}}, true},
		{"degenerate (2 unique)", [][]float64{{0, 0}, {1, 1}, {1, 1}, {0, 0}}, true},
		{"nil", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidatePolygon(tc.coords)
			if (len(got) > 0) != tc.wantErr {
				t.Fatalf("ValidatePolygon() problems = %v, wantErr %v", got, tc.wantErr)
			}
		})
	}
}

func TestPolygonWKTSwapsAxisOrder(t *testing.T) {
	got := PolygonWKT([][]float64{{1.5, 2.5}, {3, 4}, {5, 6}, {1.5, 2.5}})
	want := "POLYGON((2.5 1.5, 4 3, 6 5, 2.5 1.5))"
	if got != want {
		t.Fatalf("PolygonWKT() = %q, want %q", got, want)
	}
}

func TestLatLngBounds(t *testing.T) {
	if !ValidLatitude(90) || !ValidLatitude(-90) || ValidLatitude(90.0001) {
		t.Error("latitude bounds wrong")
	}
	if !ValidLongitude(180) || !ValidLongitude(-180) || ValidLongitude(-180.1) {
		t.Error("longitude bounds wrong")
	}
}

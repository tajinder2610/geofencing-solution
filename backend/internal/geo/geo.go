// Package geo contains pure geometry helpers: input validation and WKT encoding.
// Point-in-polygon evaluation itself is delegated to PostGIS (ST_Covers with a GiST index).
package geo

import (
	"fmt"
	"strconv"
	"strings"
)

// MinPolygonPoints is the minimum number of coordinates, including the closing point.
const MinPolygonPoints = 4

// ValidLatitude reports whether lat is within [-90, 90].
func ValidLatitude(lat float64) bool { return lat >= -90 && lat <= 90 }

// ValidLongitude reports whether lng is within [-180, 180].
func ValidLongitude(lng float64) bool { return lng >= -180 && lng <= 180 }

// ValidatePolygon checks a [lat, lng] ring and returns every problem found.
func ValidatePolygon(coords [][]float64) []string {
	var problems []string
	if len(coords) < MinPolygonPoints {
		return []string{fmt.Sprintf("coordinates must contain at least %d points (3 unique + closing point)", MinPolygonPoints)}
	}
	for i, c := range coords {
		if len(c) != 2 {
			problems = append(problems, fmt.Sprintf("coordinates[%d] must be a [latitude, longitude] pair", i))
			continue
		}
		if !ValidLatitude(c[0]) {
			problems = append(problems, fmt.Sprintf("coordinates[%d]: latitude %v must be between -90 and 90", i, c[0]))
		}
		if !ValidLongitude(c[1]) {
			problems = append(problems, fmt.Sprintf("coordinates[%d]: longitude %v must be between -180 and 180", i, c[1]))
		}
	}
	if len(problems) > 0 {
		return problems
	}
	first, last := coords[0], coords[len(coords)-1]
	if first[0] != last[0] || first[1] != last[1] {
		problems = append(problems, "polygon must be closed: first and last coordinates must be identical")
	}
	if uniquePoints(coords) < 3 {
		problems = append(problems, "polygon must have at least 3 unique points")
	}
	return problems
}

func uniquePoints(coords [][]float64) int {
	seen := make(map[[2]float64]struct{}, len(coords))
	for _, c := range coords {
		seen[[2]float64{c[0], c[1]}] = struct{}{}
	}
	return len(seen)
}

// PolygonWKT converts a validated [lat, lng] ring into WKT. WKT axis order is (lng lat).
func PolygonWKT(coords [][]float64) string {
	var b strings.Builder
	b.WriteString("POLYGON((")
	for i, c := range coords {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.FormatFloat(c[1], 'f', -1, 64))
		b.WriteByte(' ')
		b.WriteString(strconv.FormatFloat(c[0], 'f', -1, 64))
	}
	b.WriteString("))")
	return b.String()
}

package promcache

import "github.com/prometheus/common/model"

// TrimMatrix trims the series from the matrix to only contain data within the range
func TrimMatrix(matrix model.Matrix, rangeStart, rangeEnd model.Time) {
	// Trim data for the actual end/start
	// Check the datapoints, to ensure that all values are within the specified range
	for _, stream := range matrix {
		iStart := -1
		iEnd := len(stream.Values) - 1

		// trim end
		for i := iEnd; i >= 0; i-- {
			value := stream.Values[i]
			if value.Timestamp.Before(rangeEnd) || value.Timestamp.Equal(rangeEnd) {
				iEnd = i
				break
			}
		}

		// trim beginning
		for i, value := range stream.Values {
			if value.Timestamp.After(rangeStart) || value.Timestamp.Equal(rangeStart) {
				iStart = i
				break
			}
		}

		if iStart < 0 {
			panic("what")
		}
		stream.Values = stream.Values[iStart : iEnd+1]
	}
}

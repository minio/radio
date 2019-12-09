package cache

import (
	"reflect"
	"runtime"
	"testing"
)

// Tests cache drive parsing.
func TestParseCacheDrives(t *testing.T) {
	testCases := []struct {
		driveStr         string
		expectedPatterns []string
		success          bool
	}{
		{",,,", []string{}, false},
	}

	// Valid inputs
	if runtime.GOOS == "windows" {
		testCases = append(testCases, struct {
			driveStr         string
			expectedPatterns []string
			success          bool
		}{"C:/home/drive1,C:/home/drive2,C:/home/drive3", []string{"C:/home/drive1", "C:/home/drive2", "C:/home/drive3"}, true})
		testCases = append(testCases, struct {
			driveStr         string
			expectedPatterns []string
			success          bool
		}{"C:/home/drive{1...3}", []string{"C:/home/drive1", "C:/home/drive2", "C:/home/drive3"}, true})
		testCases = append(testCases, struct {
			driveStr         string
			expectedPatterns []string
			success          bool
		}{"C:/home/drive{1..3}", []string{}, false})
	} else {
		testCases = append(testCases, struct {
			driveStr         string
			expectedPatterns []string
			success          bool
		}{"/home/drive1,/home/drive2,/home/drive3", []string{"/home/drive1", "/home/drive2", "/home/drive3"}, true})
		testCases = append(testCases, struct {
			driveStr         string
			expectedPatterns []string
			success          bool
		}{"/home/drive{1...3}", []string{"/home/drive1", "/home/drive2", "/home/drive3"}, true})
		testCases = append(testCases, struct {
			driveStr         string
			expectedPatterns []string
			success          bool
		}{"/home/drive{1..3}", []string{}, false})
	}
	for i, testCase := range testCases {
		drives, err := parseCacheDrives(testCase.driveStr)
		if err != nil && testCase.success {
			t.Errorf("Test %d: Expected success but failed instead %s", i+1, err)
		}
		if err == nil && !testCase.success {
			t.Errorf("Test %d: Expected failure but passed instead", i+1)
		}
		if err == nil {
			if !reflect.DeepEqual(drives, testCase.expectedPatterns) {
				t.Errorf("Test %d: Expected %v, got %v", i+1, testCase.expectedPatterns, drives)
			}
		}
	}
}

// Tests cache exclude parsing.
func TestParseCacheExclude(t *testing.T) {
	testCases := []struct {
		excludeStr       string
		expectedPatterns []string
		success          bool
	}{
		// Invalid input
		{"/home/drive1,/home/drive2,/home/drive3", []string{}, false},
		{"/", []string{}, false},
		{",,,", []string{}, false},

		// valid input
		{"bucket1/*,*.png,images/trip/barcelona/*", []string{"bucket1/*", "*.png", "images/trip/barcelona/*"}, true},
		{"bucket1", []string{"bucket1"}, true},
	}

	for i, testCase := range testCases {
		excludes, err := parseCacheExcludes(testCase.excludeStr)
		if err != nil && testCase.success {
			t.Errorf("Test %d: Expected success but failed instead %s", i+1, err)
		}
		if err == nil && !testCase.success {
			t.Errorf("Test %d: Expected failure but passed instead", i+1)
		}
		if err == nil {
			if !reflect.DeepEqual(excludes, testCase.expectedPatterns) {
				t.Errorf("Test %d: Expected %v, got %v", i+1, testCase.expectedPatterns, excludes)
			}
		}
	}
}

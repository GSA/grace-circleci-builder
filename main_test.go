package main

import (
	"errors"
	"os"
	"testing"
)

func TestParseEntries(t *testing.T) {
	tests := []struct {
		Name         string
		File         string
		Err          error
		Expect       []*entry
		ExpectLength int
	}{{
		Name: "file does not exist",
		File: "test_data/not_here.json",
		Err: &os.PathError{
			Op:   "stat",
			Path: "test_data/not_here.json",
			Err:  errors.New("no such file or directory"),
		},
		Expect:       []*entry{},
		ExpectLength: 0,
	},
		{
			Name:         "invalid json",
			File:         "test_data/invalid.json",
			Err:          errors.New("EOF"),
			Expect:       []*entry{},
			ExpectLength: 0,
		},
		{
			Name:         "two entries",
			File:         "test_data/test.json",
			Err:          nil,
			Expect:       []*entry{{Name: "test1"}, {Name: "test2"}},
			ExpectLength: 2,
		}}
	for _, st := range tests {
		tc := st
		t.Run(tc.Name, func(t *testing.T) {
			got, err := parseEntries(tc.File)
			if err == nil && tc.Err != nil {
				t.Errorf("parseEntries() failed: expected error %v (%T)\nGot: %v (%T)\n", tc.Err, tc.Err, err, err)
			} else if err != nil && tc.Err == nil {
				t.Errorf("parseEntries() failed: did not expect error %v (%T)\n", err, err)
			}
			if tc.ExpectLength != len(got) {
				t.Errorf("parseEntries() failed: expected %v (%T)\nGot: %v (%T)\n", tc.Expect, tc.Expect, got, got)
			}
		})
	}
}

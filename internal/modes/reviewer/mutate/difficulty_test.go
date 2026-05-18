// SPDX-License-Identifier: MIT

package mutate

import "testing"

func TestProfileDifficultyScoresBoundaryAsLocalAndBasic(t *testing.T) {
	profile := ProfileDifficulty(Mutation{
		Operator:  "boundary",
		StartLine: 12,
		EndLine:   12,
	})

	if profile.Locality.Score != 1 || profile.Subtlety.Score != 1 || profile.Knowledge.Score != 1 {
		t.Fatalf("profile = %+v, want all scores at 1", profile)
	}
	if profile.Summary != "white belt" {
		t.Fatalf("summary = %q, want white belt", profile.Summary)
	}
}

func TestProfileDifficultyScoresErrorPathAsHarder(t *testing.T) {
	profile := ProfileDifficulty(Mutation{
		Operator:  "errordrop",
		StartLine: 20,
		EndLine:   25,
	})

	if profile.Locality.Score != 3 {
		t.Fatalf("locality score = %d, want 3", profile.Locality.Score)
	}
	if profile.Subtlety.Score != 3 {
		t.Fatalf("subtlety score = %d, want 3", profile.Subtlety.Score)
	}
	if profile.Knowledge.Score != 3 {
		t.Fatalf("knowledge score = %d, want 3", profile.Knowledge.Score)
	}
	if profile.Summary != "black belt" {
		t.Fatalf("summary = %q, want black belt", profile.Summary)
	}
}

func TestProfileDifficultyScoresCollectionSemantics(t *testing.T) {
	profile := ProfileDifficulty(Mutation{
		Operator:  "slicebounds",
		StartLine: 8,
		EndLine:   9,
	})

	if profile.Locality.Score != 2 {
		t.Fatalf("locality score = %d, want 2", profile.Locality.Score)
	}
	if profile.Subtlety.Score != 1 {
		t.Fatalf("subtlety score = %d, want 1", profile.Subtlety.Score)
	}
	if profile.Knowledge.Score != 2 {
		t.Fatalf("knowledge score = %d, want 2", profile.Knowledge.Score)
	}
	if profile.Summary != "green belt" {
		t.Fatalf("summary = %q, want green belt", profile.Summary)
	}
}

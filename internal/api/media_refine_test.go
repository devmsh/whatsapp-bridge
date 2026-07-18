package api

import "testing"

// The refiner's output must be classified by what actually happened, not by
// "did the text change". Conflating the two is what made the backfill loop
// unbounded: a cleaner with nothing to clean returns the text unchanged, which
// was read as failure, so the row was never marked and came back every 15s.
func TestClassifyRefineResult(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		refined string
		want    refineAction
	}{
		// The three real transcripts that looped for eight days.
		{"short arabic unchanged", "ده", "ده", refineComplete},
		{"repeated word unchanged", "موسيقى موسيقى", "موسيقى موسيقى", refineComplete},
		{"already punctuated", "نفس الشيء.", "نفس الشيء.", refineComplete},

		{"whitespace-only difference", "hello world", "  hello world  ", refineComplete},
		{"genuinely cleaned", "ok so i said", "OK, so I said.", refineStore},
		{"empty output is failure", "some raw text", "", refineRetry},
		{"whitespace output is failure", "some raw text", "   \n ", refineRetry},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRefineResult(tc.raw, tc.refined); got != tc.want {
				t.Fatalf("classifyRefineResult(%q, %q) = %v, want %v",
					tc.raw, tc.refined, got, tc.want)
			}
		})
	}
}

// Transcripts too short to punctuate should never reach the refiner at all —
// the call cannot improve them and costs bandwidth and quota.
func TestWorthRefining(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"two runes", "ده", false},
		{"ten runes with period", "نفس الشيء.", false},
		{"thirteen runes", "موسيقى موسيقى", false},
		{"empty", "", false},
		{"padded short text", "   ok   ", false},
		{"long enough arabic", "السلام عليكم كيف حالك اليوم يا صديقي", true},
		{"long enough english", "hey so i wanted to ask you about the meeting", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := worthRefining(tc.raw); got != tc.want {
				t.Fatalf("worthRefining(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

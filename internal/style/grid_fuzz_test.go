package style

import "testing"

func FuzzParseGridTrackListStaysBounded(f *testing.F) {
	f.Add("subgrid")
	f.Add("subgrid [a] repeat(auto-fill,[b]) [c]")
	f.Add("repeat(auto-fit,minmax(10px,1fr))")
	f.Add("[start] minmax(min-content,1fr) [end]")
	f.Fuzz(func(t *testing.T, source string) {
		list, ok := parseGridTrackList(source, 16, Viewport{Width: 800, Height: 600})
		if !ok {
			return
		}
		if list.Len() > maxGridTrackListEntries {
			t.Fatalf("parsed %d tracks, limit %d", list.Len(), maxGridTrackListEntries)
		}
		if list.IsSubgrid() {
			resolved := list.ResolvedSubgridLineNames(maxGridTrackListEntries + 1)
			if resolved != nil && len(resolved) != maxGridTrackListEntries+1 {
				t.Fatalf("resolved %d subgrid lines", len(resolved))
			}
		}
	})
}

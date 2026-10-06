package main

// selectTag returns the highest stable tag that is newer than current and uses
// the same "v" prefix style. changed is false when current should stay: no
// newer tag exists, or current is not a version (for example a branch name).
func selectTag(current string, tags []string) (string, bool) {
	cur, ok := parseTag(current)
	if !ok {
		return "", false
	}
	best, bestTag := cur, ""
	for _, tag := range tags {
		v, ok := parseTag(tag)
		if !ok || v.prefix != cur.prefix {
			continue
		}
		if compare(v.nums, best.nums) > 0 {
			best, bestTag = v, tag
		}
	}
	return bestTag, bestTag != ""
}

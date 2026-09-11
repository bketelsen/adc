package adc

import (
	"encoding/json"
	"fmt"
	"sort"
)

func documentHistory(s *Store, current Document) []Document {
	out := []Document{current}
	records, err := s.Records("revision", current.Org)
	if err != nil {
		return out
	}
	for _, record := range records {
		if record.Parent != current.ID {
			continue
		}
		var d Document
		if json.Unmarshal(record.Data, &d) == nil {
			d.ID = current.ID
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision > out[j].Revision })
	return out
}

func documentVersion(s *Store, current Document, revision int) (Document, error) {
	for _, d := range documentHistory(s, current) {
		if d.Revision == revision {
			return d, nil
		}
	}
	return Document{}, fmt.Errorf("document revision is unavailable; reload and select the passage again")
}

func teamProposalDocument(d Decision) Document {
	return Document{ID: "team-brief:" + d.ID, Org: d.Org, Task: d.Task, Run: d.Run, Title: "Proposed team — responsibilities and evidence", Content: d.Question, Source: "/task?org=" + d.Org + "&id=" + d.Task, Created: now(), Revision: 1}
}

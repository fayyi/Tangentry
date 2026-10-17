package discourse

import "testing"

func TestLatestTopics(t *testing.T) {
	feed, err := Parse([]byte(`{"topic_list":{"more_topics_url":"/latest.json?page=1","topics":[{"id":42,"title":"测试话题","created_at":"2026-10-12T12:00:00Z"},{"id":-1,"title":"invalid"}]}}`), 1)
	if err != nil || len(feed.Items) != 1 || !feed.HasMore || feed.Items[0].ID != "linuxdo:42" || feed.Items[0].PublishedAt <= 0 || feed.Items[0].URL != "https://linux.do/t/42" {
		t.Fatalf("invalid topic mapping: %+v %v", feed, err)
	}
	for _, raw := range []string{`{}`, `<html>challenge</html>`, `{"topic_list":null}`} {
		if _, err := Parse([]byte(raw), 1); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
}

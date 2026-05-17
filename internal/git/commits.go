package git

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type CommitResponse struct {
	SHA string `json:"sha"`
}

func GetLatestCommit(repo string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/commits/main", repo)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("fetching commits for %s: %w", repo, err)
	}
	defer resp.Body.Close()

	var data CommitResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	return data.SHA, nil
}

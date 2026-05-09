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

	url := fmt.Sprintf(
		"https://api.github.com/repos/%s/commits/main",
		repo,
	)

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data CommitResponse

	err = json.NewDecoder(resp.Body).Decode(&data)
	if err != nil {
		return "", err
	}

	return data.SHA, nil
}

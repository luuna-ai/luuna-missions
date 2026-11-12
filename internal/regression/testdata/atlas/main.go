package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	raw, _ := json.Marshal(args)
	f, err := os.OpenFile("atlas-invocations.jsonl", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	fmt.Fprintln(f, string(raw))
	f.Close()
	if len(args) > 1 && args[0] == "--json" && args[1] == "status" {
		fmt.Print(`{"domains":[{"domain":"parity"}]}`)
	} else if len(args) > 1 && args[0] == "--json" && args[1] == "query" {
		kind := ""
		for i, a := range args {
			if a == "--type" && i+1 < len(args) {
				kind = args[i+1]
			}
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"domains": []any{map[string]any{"domain": "parity", "records": []any{map[string]any{"id": "atlas-" + kind, "type": kind, "name": "Parity " + kind, "description": "Grounded record"}}}}})
	} else if len(args) > 0 && args[0] == "record" {
		fmt.Print(`{"record":{"id":"atlas-decision-record"}}`)
	} else {
		os.Exit(2)
	}
}

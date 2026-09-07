package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// runKnowledgeCommandWithConfig is the backend-aware CLI entrypoint. JSON
// keeps the historical updater; PostgreSQL uses the same typed mutation
// service as dashboard and other application callers.
func runKnowledgeCommandWithConfig(args []string, cfg Config, out io.Writer) error {
	backend, err := normalizeStorageBackend(cfg.StorageBackend)
	if err != nil {
		return err
	}
	if backend == storageBackendJSON {
		return runKnowledgeCommand(args, cfg.CandidateProfilePath, out)
	}
	persistence, closePersistence, err := BuildCandidatePersistence(context.Background(), cfg)
	if err != nil {
		return err
	}
	defer closePersistence()
	if len(args) == 0 {
		return knowledgeUsageError()
	}
	candidate, err := persistence.Repository.CurrentCandidate(context.Background())
	if err != nil {
		return err
	}
	switch args[0] {
	case "proposals":
		if len(args) != 1 {
			return knowledgeUsageError()
		}
		pending := make([]KnowledgeProposal, 0)
		for _, proposal := range candidate.Proposals {
			if proposal.Status == KnowledgeProposalPending {
				pending = append(pending, proposal)
			}
		}
		return json.NewEncoder(out).Encode(pending)
	case "confirm", "reject":
		if len(args) != 2 || args[1] == "" {
			return knowledgeUsageError()
		}
		command := ResolveKnowledgeProposalCommand{Actor: KnowledgeActorUser, ProposalID: args[1]}
		if args[0] == "confirm" {
			err = persistence.Mutations.ConfirmProposal(context.Background(), command)
		} else {
			err = persistence.Mutations.RejectProposal(context.Background(), command)
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "Proposal %s: %s\n", args[1], kbProposalStatus(args[0]))
		return err
	case "sync-profile":
		return errors.New("profile knowledge sync-profile is legacy-only in PostgreSQL mode; use an explicit candidate resume import")
	default:
		return knowledgeUsageError()
	}
}

func knowledgeUsageError() error {
	return errors.New("usage: profile knowledge [proposals|sync-profile|confirm <id>|reject <id>] [-candidate-profile path]")
}

func runKnowledgeCommand(args []string, path string, out io.Writer) error {
	if len(args) == 0 {
		return knowledgeUsageError()
	}
	switch args[0] {
	case "proposals":
		if len(args) != 1 {
			return knowledgeUsageError()
		}
	case "sync-profile":
		if len(args) != 1 {
			return knowledgeUsageError()
		}
	case "confirm", "reject":
		if len(args) != 2 || args[1] == "" {
			return knowledgeUsageError()
		}
	default:
		return knowledgeUsageError()
	}
	kb := NewCandidateKnowledgeBase(path)
	if err := kb.Load(); err != nil {
		return err
	}
	if args[0] == "proposals" {
		pending := make([]KnowledgeProposal, 0)
		for _, proposal := range kb.Proposals {
			if proposal.Status == KnowledgeProposalPending {
				pending = append(pending, proposal)
			}
		}
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(pending)
	}
	if args[0] == "sync-profile" {
		result, err := kb.SyncProfileKnowledge()
		if err != nil {
			return err
		}
		if err := kb.Save(); err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(result)
	}
	updater := NewCandidateKnowledgeUpdater(kb, KnowledgeUpdaterOptions{Actor: KnowledgeActorUser})
	var err error
	if args[0] == "confirm" {
		err = updater.ConfirmKnowledge(args[1])
	} else {
		err = updater.RejectKnowledge(args[1])
	}
	if err != nil {
		return err
	}
	if err := kb.Save(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Proposal %s: %s\n", args[1], kbProposalStatus(args[0]))
	return err
}

func kbProposalStatus(command string) KnowledgeProposalStatus {
	if command == "confirm" {
		return KnowledgeProposalConfirmed
	}
	return KnowledgeProposalRejected
}

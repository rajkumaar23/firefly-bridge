// backfill-hashes sets the internal_reference field on every existing Firefly
// transaction that is missing it, using the same SHA-256 hash algorithm that
// firefly-bridge uses during normal ingestion. Run this tool once against an
// existing Firefly database before enabling firefly-bridge so that future runs
// do not create duplicate transactions.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/rajkumaar23/firefly-bridge/internal/firefly"
	"github.com/rajkumaar23/firefly-bridge/internal/utils"
	"github.com/sirupsen/logrus"
)

func main() {
	var debug = flag.Bool("debug", false, "enable debug logs")
	var host = flag.String("host", "", "firefly host (eg: http://firefly.lan.example.com), alternative to $FIREFLY_HOST")
	var token = flag.String("token", "", "firefly access token, alternative to $FIREFLY_TOKEN")
	var concurrency = flag.Int("concurrency", 5, "max concurrent update requests per account")
	flag.Parse()

	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true, ForceColors: true})
	if *debug {
		logger.SetLevel(logrus.DebugLevel)
		logger.Debug("debug logging enabled")
	}

	ctx := context.Background()
	ctx = utils.WithLogger(ctx, logger)

	if *host == "" {
		if h := os.Getenv("FIREFLY_HOST"); h != "" {
			*host = h
		} else {
			logger.Panicf("firefly host not provided; set --host or $FIREFLY_HOST")
		}
	}
	if *token == "" {
		if t := os.Getenv("FIREFLY_TOKEN"); t != "" {
			*token = t
		} else {
			logger.Panicf("firefly token not provided; set --token or $FIREFLY_TOKEN")
		}
	}

	ff, err := firefly.NewAPIClient(ctx, *host, *token)
	if err != nil {
		logger.Panicf("failed to connect to firefly: %s", err)
	}
	logger.Info("connected to firefly")

	accounts, err := listAllAccounts(ctx, ff)
	if err != nil {
		logger.Panicf("failed to list accounts: %s", err)
	}
	logger.Infof("found %d asset account(s)", len(accounts))

	reader := bufio.NewReader(os.Stdin)

	// Track transaction group IDs already updated to avoid double-processing a
	// group that appears in multiple accounts' transaction lists (e.g. transfers).
	updatedIDs := make(map[string]bool)

	totalAccountsUpdated := 0
	totalGroupsUpdated := 0
	totalSplitsUpdated := 0

	for _, account := range accounts {
		aLog := logger.WithField("account", account.Attributes.Name)

		txns, err := listAllTransactionsForAccount(ctx, ff, account.Id)
		if err != nil {
			aLog.Errorf("failed to list transactions: %s", err)
			continue
		}

		// Collect groups that have at least one split missing internal_reference.
		// Groups containing opening-balance or reconciliation splits are skipped:
		// those types are rejected by the update endpoint and are never imported
		// by firefly-bridge anyway.
		type groupToUpdate struct {
			txn        firefly.TransactionRead
			splitCount int // number of splits in this group missing internal_reference
		}
		var toUpdate []groupToUpdate

		for _, txn := range txns {
			if updatedIDs[txn.Id] {
				continue
			}
			skippable := false
			for _, split := range txn.Attributes.Transactions {
				if !isUpdatableType(split.Type) {
					skippable = true
					break
				}
			}
			if skippable {
				aLog.Debugf("skipping transaction group %s (contains non-updatable split type)", txn.Id)
				continue
			}
			missing := 0
			for _, split := range txn.Attributes.Transactions {
				if split.InternalReference == nil || *split.InternalReference == "" {
					missing++
				}
			}
			if missing > 0 {
				toUpdate = append(toUpdate, groupToUpdate{txn: txn, splitCount: missing})
			}
		}

		if len(toUpdate) == 0 {
			aLog.Infof("all transactions already have internal_reference set — skipping")
			continue
		}

		totalSplits := 0
		for _, g := range toUpdate {
			totalSplits += g.splitCount
		}

		fmt.Printf("\n[%s] %d transaction group(s) | %d split(s) missing internal_reference\n",
			account.Attributes.Name, len(toUpdate), totalSplits)
		fmt.Print("Update? (y/n): ")

		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(line)) != "y" {
			aLog.Info("skipped by user")
			continue
		}

		updated := 0
		splitsUpdated := 0
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, *concurrency)

		for _, g := range toUpdate {
			mu.Lock()
			alreadyDone := updatedIDs[g.txn.Id]
			mu.Unlock()
			if alreadyDone {
				continue
			}

			wg.Add(1)
			sem <- struct{}{}
			go func(g groupToUpdate) {
				defer wg.Done()
				defer func() { <-sem }()

				if err := updateGroupInternalReferences(ctx, ff, g.txn, aLog, *host); err != nil {
					aLog.Errorf("failed to update transaction group %s: %s", g.txn.Id, err)
					return
				}
				mu.Lock()
				updatedIDs[g.txn.Id] = true
				updated++
				splitsUpdated += g.splitCount
				mu.Unlock()
			}(g)
		}
		wg.Wait()

		aLog.Infof("updated %d group(s), %d split(s)", updated, splitsUpdated)
		totalAccountsUpdated++
		totalGroupsUpdated += updated
		totalSplitsUpdated += splitsUpdated
	}

	fmt.Printf("\nDone. %d account(s) | %d transaction group(s) | %d split(s) updated.\n",
		totalAccountsUpdated, totalGroupsUpdated, totalSplitsUpdated)
}

func listAllAccounts(ctx context.Context, ff *firefly.ClientWithResponses) ([]firefly.AccountRead, error) {
	var all []firefly.AccountRead
	page := int32(1)
	limit := int32(100)
	accountType := firefly.AccountTypeFilterAsset

	for {
		res, err := ff.ListAccountWithResponse(ctx, &firefly.ListAccountParams{
			Page:  &page,
			Limit: &limit,
			Type:  &accountType,
		})
		if err != nil {
			return nil, err
		}
		if res.ApplicationvndApiJSON200 == nil {
			return nil, fmt.Errorf("unexpected status %s", res.Status())
		}
		all = append(all, res.ApplicationvndApiJSON200.Data...)

		meta := res.ApplicationvndApiJSON200.Meta
		if meta.Pagination == nil || meta.Pagination.TotalPages == nil || int(page) >= *meta.Pagination.TotalPages {
			break
		}
		page++
	}
	return all, nil
}

func listAllTransactionsForAccount(ctx context.Context, ff *firefly.ClientWithResponses, accountID string) ([]firefly.TransactionRead, error) {
	var all []firefly.TransactionRead
	page := int32(1)
	limit := int32(100)

	for {
		res, err := ff.ListTransactionByAccountWithResponse(ctx, accountID, &firefly.ListTransactionByAccountParams{
			Page:  &page,
			Limit: &limit,
		})
		if err != nil {
			return nil, err
		}
		if res.ApplicationvndApiJSON200 == nil {
			return nil, fmt.Errorf("unexpected status %s", res.Status())
		}
		all = append(all, res.ApplicationvndApiJSON200.Data...)

		meta := res.ApplicationvndApiJSON200.Meta
		if meta.Pagination == nil || meta.Pagination.TotalPages == nil || int(page) >= *meta.Pagination.TotalPages {
			break
		}
		page++
	}
	return all, nil
}

// isUpdatableType returns true for the transaction types that the Firefly update
// endpoint accepts. opening_balance and reconciliation are managed by Firefly
// internally and cannot be updated via PUT /transactions/{id}.
func isUpdatableType(t firefly.TransactionTypeProperty) bool {
	switch t {
	case firefly.Withdrawal, firefly.Deposit, firefly.Transfer:
		return true
	default:
		return false
	}
}

// updateGroupInternalReferences sends an UpdateTransaction request that sets
// internal_reference on every split in the group that is currently empty.
// All splits are always included in the request body to avoid unintentional removal.
// A raw JSON body is used so that only the fields we explicitly set are included —
// the typed struct serialises nil pointer fields as JSON null, which Firefly treats
// as explicit clears and would wipe category, tags, notes, etc.
func updateGroupInternalReferences(ctx context.Context, ff *firefly.ClientWithResponses, txn firefly.TransactionRead, log *logrus.Entry, host string) error {
	type splitBody map[string]any

	splits := make([]splitBody, 0, len(txn.Attributes.Transactions))

	for _, split := range txn.Attributes.Transactions {
		ref := split.InternalReference
		if ref == nil || *ref == "" {
			hash, err := split.HashV2()
			if err != nil {
				return fmt.Errorf("failed to compute hash: %w", err)
			}
			ref = &hash
			log.Debugf("%s/transactions/show/%s | set %s | %s | %s | %q",
				strings.TrimSuffix(host, "/"), txn.Id, hash, split.Date.Format("2006-01-02"), split.Amount, split.Description)
		}

		su := splitBody{
			"transaction_journal_id": split.TransactionJournalId,
			"internal_reference":     *ref,
		}
		splits = append(splits, su)
	}

	bodyMap := map[string]any{
		"transactions":  splits,
		"apply_rules":   false,
		"fire_webhooks": false,
	}
	if gt := txn.Attributes.GroupTitle; gt != nil && *gt != "" {
		bodyMap["group_title"] = *gt
	} else if len(splits) > 1 {
		bodyMap["group_title"] = txn.Attributes.Transactions[0].Description
	}

	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	res, err := ff.UpdateTransactionWithBodyWithResponse(ctx, txn.Id, &firefly.UpdateTransactionParams{}, "application/json", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("API call failed: %w", err)
	}
	if res.ApplicationvndApiJSON200 == nil {
		return fmt.Errorf("unexpected status (%s): %s", res.Status(), string(res.Body))
	}
	return nil
}

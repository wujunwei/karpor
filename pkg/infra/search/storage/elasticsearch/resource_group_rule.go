// Copyright The Karpor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/KusionStack/karpor/pkg/core/entity"
	"github.com/KusionStack/karpor/pkg/infra/search/storage"
	"github.com/elliotxx/esquery"
)

const (
	resourceGroupRuleKeyID          = "id"
	resourceGroupRuleKeyName        = "name"
	resourceGroupRuleKeyDescription = "description"
	resourceGroupRuleKeyFields      = "fields"
	resourceGroupRuleKeyCreatedAt   = "createdAt"
	resourceGroupRuleKeyUpdatedAt   = "updatedAt"
	resourceGroupRuleKeyDeletedAt   = "deletedAt"
)

// DeleteResourceGroupRule deletes a resource group rule based on the given name.
func (s *Storage) DeleteResourceGroupRule(ctx context.Context, name string) error {
	// Refresh the index before searching to ensure real-time data.
	err := s.client.Refresh(ctx, s.resourceGroupRuleIndexName)
	if err != nil {
		return err
	}

	if rgr, err := s.GetResourceGroupRule(ctx, name); err != nil {
		return err
	} else {
		return s.client.DeleteDocument(ctx, s.resourceGroupRuleIndexName, rgr.ID)
	}
}

// GetResourceGroupRule retrieves a resource group rule based on the given name.
func (s *Storage) GetResourceGroupRule(ctx context.Context, name string) (*entity.ResourceGroupRule, error) {
	// Refresh the index before searching to ensure real-time data.
	err := s.client.Refresh(ctx, s.resourceGroupRuleIndexName)
	if err != nil {
		return nil, err
	}

	query := generateResourceGroupRuleQuery(name)
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(query); err != nil {
		return nil, err
	}
	resp, err := s.client.SearchDocument(ctx, s.resourceGroupRuleIndexName, buf)
	if err != nil {
		return nil, err
	}

	if resp.Hits.Total.Value == 0 {
		return nil, storage.ErrResourceGroupRuleNotFound
	}

	res, err := storage.Map2ResourceGroupRule(resp.Hits.Hits[0].Source)
	if err != nil {
		return nil, err
	}

	return res, nil
}

// ListResourceGroupRules lists all resource group rules by searching the entire
// index.
func (s *Storage) ListResourceGroupRules(ctx context.Context) ([]*entity.ResourceGroupRule, error) {
	// Refresh the index before searching to ensure real-time data.
	err := s.client.Refresh(ctx, s.resourceGroupRuleIndexName)
	if err != nil {
		return nil, err
	}

	// Create a query to search for all resource group rules.
	query := generateResourceGroupRuleQueryForAll()

	// Buffer to hold the query JSON.
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(query); err != nil {
		return nil, err
	}

	// Execute the search document call to the storage.
	resp, err := s.client.SearchDocument(ctx, s.resourceGroupRuleIndexName, buf)
	if err != nil {
		return nil, err
	}

	// Check if the search found any resource group rules.
	if resp.Hits.Total.Value == 0 {
		return nil, storage.ErrResourceGroupRuleNotFound
	}

	// Initialize a slice to hold the resource group rules.
	rgrList := make([]*entity.ResourceGroupRule, 0, len(resp.Hits.Hits))

	// Iterate over the search hits and map each hit to a ResourceGroupRule entity.
	for _, hit := range resp.Hits.Hits {
		// Map the source of the hit to a ResourceGroupRule entity.
		rgr, err := storage.Map2ResourceGroupRule(hit.Source)
		if err != nil {
			return nil, err
		}
		rgrList = append(rgrList, rgr)
	}

	return rgrList, nil
}

// ListResourceGroupsBy lists all resource groups by specified resource group
// rule name.
func (s *Storage) ListResourceGroupsBy(ctx context.Context, ruleName string) (*storage.ResourceGroupResult, error) {
	// Refresh the index before searching to ensure real-time data.
	err := s.client.Refresh(ctx, s.resourceGroupRuleIndexName)
	if err != nil {
		return nil, err
	}

	rgr, err := s.GetResourceGroupRule(ctx, ruleName)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.AggregateDocumentByTerms(ctx, s.resourceIndexName, rgr.Fields)
	if err != nil {
		return nil, err
	}

	// Check if the search found any resource groups.
	if resp.Total == 0 {
		return nil, storage.ErrResourceGroupNotFound
	}

	// Initialize a slice to hold the resource group rules.
	rgList := make([]*entity.ResourceGroup, 0, len(resp.Buckets))

	// Iterate over the search hits and map each hit to a ResourceGroupRule entity.
	for _, bucket := range resp.Buckets {
		if len(rgr.Fields) != len(bucket.Keys) {
			return nil, fmt.Errorf("mismatched number of fields: expected %d, got %d", len(rgr.Fields), len(bucket.Keys))
		}
		// Convert the current bucket to a resource group.
		rg := &entity.ResourceGroup{}
		for i, v := range bucket.Keys {
			field := rgr.Fields[i]
			switch field {
			case "cluster":
				rg.Cluster = v
			case "apiVersion":
				rg.APIVersion = v
			case "kind":
				rg.Kind = v
			case "namespace":
				rg.Namespace = v
			case "name":
				rg.Name = v
			default:
				if strings.HasPrefix(field, "annotations.") {
					annoKey := strings.TrimPrefix(field, "annotations.")
					if rg.Annotations == nil {
						rg.Annotations = map[string]string{annoKey: v}
					} else {
						rg.Annotations[annoKey] = v
					}
				} else if strings.HasPrefix(field, "labels.") {
					labelKey := strings.TrimPrefix(field, "labels.")
					if rg.Labels == nil {
						rg.Labels = map[string]string{labelKey: v}
					} else {
						rg.Labels[labelKey] = v
					}
				}
			}
		}
		rgList = append(rgList, rg)
	}

	return &storage.ResourceGroupResult{
		Groups: rgList,
		Fields: rgr.Fields,
	}, nil
}

// SaveResourceGroupRule saves a resource group rule to the storage.
func (s *Storage) SaveResourceGroupRule(ctx context.Context, data *entity.ResourceGroupRule) error {
	id, body, err := s.generateResourceGroupRuleDocument(data)
	if err != nil {
		return err
	}
	return s.client.SaveDocument(ctx, s.resourceGroupRuleIndexName, id, bytes.NewReader(body))
}

// CountResourceGroupRules return a count of resource group rules in the
// Elasticsearch storage.
func (s *Storage) CountResourceGroupRules(ctx context.Context) (int, error) {
	if resp, err := s.client.Count(ctx, s.resourceGroupRuleIndexName); err != nil {
		return 0, err
	} else {
		return int(resp.Count), nil
	}
}

// generateResourceGroupRuleDocument creates a resource group rule document for
// Elasticsearch with the specified name, description etc.
func (s *Storage) generateResourceGroupRuleDocument(data *entity.ResourceGroupRule) (id string, body []byte, err error) {
	if len(data.ID) == 0 {
		id = entity.UUID()
	} else {
		id = data.ID
	}
	body, err = json.Marshal(map[string]interface{}{
		resourceGroupRuleKeyID:          id,
		resourceGroupRuleKeyName:        data.Name,
		resourceGroupRuleKeyDescription: data.Description,
		resourceGroupRuleKeyFields:      data.Fields,
		resourceGroupRuleKeyCreatedAt:   data.CreatedAt,
		resourceGroupRuleKeyUpdatedAt:   data.UpdatedAt,
	})
	if err != nil {
		return
	}
	return
}

// generateResourceGroupRuleQuery creates a query to search for an object in
// Elasticsearch based on resource group rule's name.
func generateResourceGroupRuleQuery(name string) map[string]interface{} {
	query := make(map[string]interface{})
	query["query"] = esquery.Bool().Must(
		esquery.Term(resourceKeyName, name),
	).Map()
	return query
}

// generateResourceGroupRuleQueryForAll creates a query to search for all
// resource group rules.
func generateResourceGroupRuleQueryForAll() map[string]interface{} {
	query := make(map[string]interface{})
	// This query will match all documents in the index.
	query["query"] = esquery.MatchAll().Map()
	return query
}

package table

import (
	"fmt"
	"strings"

	"get.porter.sh/plugin/azure/pkg/azure/azureconfig"
	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/cnabio/cnab-go/utils/crud"
	"github.com/google/uuid"
	"github.com/hashicorp/go-hclog"
)

var _ crud.Store = &Store{}

const (
	tableName = "porter"
	timeout   = 60
	maxSize   = 65536
)

// Store implements the backing store for claims in azure table storage
type Store struct {
	logger hclog.Logger
	config azureconfig.Config
	table  *storage.Table
}

func NewStore(cfg azureconfig.Config, l hclog.Logger) *Store {
	return &Store{
		config: cfg,
		logger: l,
	}
}

func (s *Store) init() error {

	storageAccountName, storageAccountKey, err := GetCredentials(s.config, s.logger)
	if err != nil {
		return err
	}
	client, err := storage.NewBasicClient(storageAccountName, storageAccountKey)
	if err != nil {
		return err
	}
	tableServiceClient := client.GetTableService()
	s.table = tableServiceClient.GetTableReference(tableName)
	err = s.table.Get(timeout, storage.MinimalMetadata)
	if err != nil {
		if strings.Contains(err.Error(), "The specified resource does not exist") {
			guid := uuid.New().String()
			s.logger.Info(fmt.Sprintf("Creating Table: %s requestId %s", tableName, guid))
			if err = s.table.Create(timeout, storage.MinimalMetadata, &storage.TableOptions{RequestID: guid}); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (s *Store) Count(itemType string, group string) (int, error) {
	err := s.init()
	if err != nil {
		return 0, err
	}
	guid := uuid.New().String()
	options := storage.QueryOptions{
		RequestID: guid,
		Filter:    fmt.Sprintf("PartitionKey eq '%s' and group eq '%s'", itemType, group),
	}

	s.logger.Info(fmt.Sprintf("Count items for %s/ group=%q requestId %s", itemType, group, guid))

	result, err := s.table.QueryEntities(timeout, storage.MinimalMetadata, &options)
	if err != nil {
		if strings.Contains(err.Error(), "The specified resource does not exist") {
			err = crud.ErrRecordDoesNotExist
		}
		return 0, err
	}

	count := len(result.Entities)

	for result.NextLink != nil {
		guid = uuid.New().String()
		s.logger.Info(fmt.Sprintf("Count nextlink items for %s/ group=%q requestId %s", itemType, group, guid))
		result, err = result.NextResults(&storage.TableOptions{RequestID: guid})
		if err != nil {
			return 0, err
		}
		count += len(result.Entities)
	}

	return count, nil
}

func (s *Store) List(itemType string, group string) ([]string, error) {
	err := s.init()
	if err != nil {
		return nil, err
	}
	guid := uuid.New().String()
	options := storage.QueryOptions{
		RequestID: guid,
		Filter:    fmt.Sprintf("PartitionKey eq '%s' and group eq '%s'", itemType, group),
	}

	s.logger.Info(fmt.Sprintf("List items for %s/ group=%q requestId %s", itemType, group, guid))

	result, err := s.table.QueryEntities(timeout, storage.MinimalMetadata, &options)
	if err != nil {
		if strings.Contains(err.Error(), "The specified resource does not exist") {
			err = crud.ErrRecordDoesNotExist
		}
		return nil, err
	}

	names := make([]string, 0, len(result.Entities))
	for {
		for _, entity := range result.Entities {
			names = append(names, entity.RowKey)
		}
		if result.NextLink == nil {
			break
		} else {
			guid = uuid.New().String()
			s.logger.Info(fmt.Sprintf("List items nextlink items for %s/ group=%q requestId %s", itemType, group, guid))
			result, err = result.NextResults(&storage.TableOptions{RequestID: guid})
			if err != nil {
				return nil, err
			}
		}
	}

	s.logger.Info(fmt.Sprintf("names: %s", strings.Join(names, ", ")))
	return names, nil
}

func (s *Store) Save(itemType string, group string, name string, data []byte) error {

	if len(data) > maxSize {
		return fmt.Errorf("Data exceeds maximum length for table storage for item: %s/ group=%q %s length: %d", itemType, group, name, len(data))
	}

	err := s.init()
	if err != nil {
		return err
	}

	if itemType == "" && name == "schema" {
		itemType = "schema"
	}

	row := s.table.GetEntityReference(itemType, name)
	p := make(map[string]interface{})
	p["group"] = group
	p["data"] = data
	row.Properties = p
	guid := uuid.New().String()
	options := storage.EntityOptions{
		Timeout:   timeout,
		RequestID: guid,
	}
	s.logger.Info(fmt.Sprintf("Save %s/ group=%q %s requestId %s", itemType, group, name, guid))

	return row.InsertOrReplace(&options)

}

func (s *Store) Read(itemType string, name string) ([]byte, error) {
	err := s.init()
	if err != nil {
		return nil, err
	}

	if itemType == "" && name == "schema" {
		itemType = "schema"
	}

	row := s.table.GetEntityReference(itemType, name)
	guid := uuid.New().String()
	options := storage.GetEntityOptions{
		RequestID: guid,
	}
	err = row.Get(timeout, storage.MinimalMetadata, &options)
	if err != nil {
		if strings.Contains(err.Error(), "The specified resource does not exist") {
			err = crud.ErrRecordDoesNotExist
		}
		return nil, err
	}

	s.logger.Info(fmt.Sprintf("Read itemtype %s %s requestId %s", itemType, name, guid))

	data, ok := row.Properties["data"].([]byte)
	if !ok {
		return nil, err
	}
	return data, err
}

func (s *Store) Delete(itemType string, name string) error {
	err := s.init()
	if err != nil {
		return err
	}

	row := s.table.GetEntityReference(itemType, name)
	guid := uuid.New().String()
	options := storage.EntityOptions{
		Timeout:   timeout,
		RequestID: guid,
	}
	s.logger.Info(fmt.Sprintf("Delete itemtype %s %s requestId %s", itemType, name, guid))
	return row.Delete(true, &options)

}

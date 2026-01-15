package inventory

import (
	"context"

	"github.com/cloudreve/Cloudreve/v4/ent"
	"github.com/cloudreve/Cloudreve/v4/ent/fsevent"
	"github.com/cloudreve/Cloudreve/v4/ent/schema"
	"github.com/cloudreve/Cloudreve/v4/pkg/conf"
	"github.com/gofrs/uuid"
	"github.com/samber/lo"
)

type FsEventClient interface {
	TxOperator
	// Create a new FsEvent
	Create(ctx context.Context, uid int, subscriberId uuid.UUID, events ...string) error
	// Delete all FsEvents by subscriber
	DeleteBySubscriber(ctx context.Context, subscriberId uuid.UUID) error
	// Delete all FsEvents
	DeleteAll(ctx context.Context) error
	// Get all FsEvents by subscriber and user
	TakeBySubscriber(ctx context.Context, subscriberId uuid.UUID, userId int) ([]*ent.FsEvent, error)
}

func NewFsEventClient(client *ent.Client, dbType conf.DBType) FsEventClient {
	return &fsEventClient{client: client, maxSQlParam: sqlParamLimit(dbType)}
}

type fsEventClient struct {
	maxSQlParam int
	client      *ent.Client
}

func (c *fsEventClient) SetClient(newClient *ent.Client) TxOperator {
	return &fsEventClient{client: newClient, maxSQlParam: c.maxSQlParam}
}

func (c *fsEventClient) GetClient() *ent.Client {
	return c.client
}

func (c *fsEventClient) Create(ctx context.Context, uid int, subscriberId uuid.UUID, events ...string) error {
	stms := lo.Map(events, func(event string, index int) *ent.FsEventCreate {
		res := c.client.FsEvent.
			Create().
			SetUserFsevent(uid).
			SetEvent(event).
			SetSubscriber(subscriberId).SetEvent(event)

		return res
	})

	_, err := c.client.FsEvent.CreateBulk(stms...).Save(ctx)
	return err
}

func (c *fsEventClient) DeleteBySubscriber(ctx context.Context, subscriberId uuid.UUID) error {
	_, err := c.client.FsEvent.Delete().Where(fsevent.Subscriber(subscriberId)).Exec(schema.SkipSoftDelete(ctx))
	return err
}

func (c *fsEventClient) DeleteAll(ctx context.Context) error {
	_, err := c.client.FsEvent.Delete().Exec(schema.SkipSoftDelete(ctx))
	return err
}

func (c *fsEventClient) TakeBySubscriber(ctx context.Context, subscriberId uuid.UUID, userId int) ([]*ent.FsEvent, error) {
	res, err := c.client.FsEvent.Query().Where(fsevent.Subscriber(subscriberId), fsevent.UserFsevent(userId)).All(ctx)
	if err != nil {
		return nil, err
	}

	// Delete the FsEvents
	_, err = c.client.FsEvent.Delete().Where(fsevent.Subscriber(subscriberId), fsevent.UserFsevent(userId)).Exec(schema.SkipSoftDelete(ctx))
	if err != nil {
		return nil, err
	}

	return res, nil
}

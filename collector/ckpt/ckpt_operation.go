package ckpt

import (
	"bytes"
	"encoding/json"
	"fmt"
	conf "github.com/alibaba/MongoShake/v2/collector/configure"
	bson2 "github.com/vinllen/mongo-go-driver/bson"
	"github.com/vinllen/mongo-go-driver/mongo"
	"github.com/vinllen/mongo-go-driver/mongo/options"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"io/ioutil"
	"net/http"

	utils "github.com/alibaba/MongoShake/v2/common"

	LOG "github.com/vinllen/log4go"
)

const (
	// we can't insert Timestamp(0, 0) that will be treat as Now() inserted
	// into mongo. so we use Timestamp(0, 1)
	InitCheckpoint  = primitive.DateTime(1)
	EmptyCheckpoint = primitive.DateTime(0)
)

type CheckpointContext struct {
	Name                   string             `bson:"name" json:"name"`
	Timestamp              primitive.DateTime `bson:"ckpt" json:"ckpt"`
	Version                int                `bson:"version" json:"version"`
	FetchMethod            string             `bson:"fetch_method" json:"fetch_method"`
	OplogDiskQueue         string             `bson:"oplog_disk_queue" json:"oplog_disk_queue"`
	OplogDiskQueueFinishTs primitive.DateTime `bson:"oplog_disk_queue_apply_finish_ts" json:"oplog_disk_queue_apply_finish_ts"`
}

func (cc *CheckpointContext) String() string {
	if ret, err := json.Marshal(cc); err != nil {
		return err.Error()
	} else {
		return string(ret)
	}
}

type CheckpointOperation interface {
	// read checkpoint from remote storage. and encapsulation
	// with CheckpointContext struct
	// bool means whether exists on remote
	Get() (*CheckpointContext, bool)

	// save checkpoint
	Insert(ckpt *CheckpointContext) error

	// log info
	String() string
}

// mongo
type MongoCheckpoint struct {
	CheckpointContext

	//Conn        *utils.MongoConn
	//QueryHandle *mgo.Collection

	client *utils.MongoCommunityConn

	// connection info
	URL       string
	DB, Table string
}

func (ckpt *MongoCheckpoint) ensureNetwork() bool {
	if ckpt.client == nil {
		if client, err := utils.NewMongoCommunityConn(ckpt.URL, utils.VarMongoConnectModePrimary, true,
			utils.ReadWriteConcernMajority, utils.ReadWriteConcernMajority,
			conf.Options.CheckpointStorageUrlMongoSslRootCaFile); err == nil {
			ckpt.client = client

		} else {
			LOG.Warn("%s CheckpointOperation manager connect mongo cluster failed. %v", ckpt.Name, err)
			return false
		}
	}

	return true
}

// deprecate
//func (ckpt *MongoCheckpoint) ensureNetworkMgo() bool {
//	// make connection if we haven't already established one
//	if ckpt.Conn == nil {
//		if conn, err := utils.NewMongoConn(ckpt.URL, utils.VarMongoConnectModePrimary, true,
//			utils.ReadWriteConcernMajority, utils.ReadWriteConcernMajority,
//			conf.Options.CheckpointStorageUrlMongoSslRootCaFile); err == nil {
//			ckpt.Conn = conn
//			ckpt.QueryHandle = conn.Session.DB(ckpt.DB).C(ckpt.Table)
//		} else {
//			LOG.Warn("%s CheckpointOperation manager connect mongo cluster failed. %v", ckpt.Name, err)
//			return false
//		}
//	}
//
//	return true
//}

func (ckpt *MongoCheckpoint) close() {
	ckpt.client.Close()
	ckpt.client = nil
}

//func (ckpt *MongoCheckpoint) close() {
//	ckpt.Conn.Close()
//	ckpt.Conn = nil
//}

func (ckpt *MongoCheckpoint) Get() (*CheckpointContext, bool) {
	if !ckpt.ensureNetwork() {
		LOG.Warn("%s Reload ckpt ensure network failed. %v", ckpt.Name, ckpt.client)
		return nil, false
	}

	var err error
	value := new(CheckpointContext)
	if err = ckpt.client.Client.Database(ckpt.DB).Collection(ckpt.Table).FindOne(nil,
		bson2.M{CheckpointName: ckpt.Name}).Decode(value); err == nil {

		LOG.Info("%s Load exist checkpoint. content %v", ckpt.Name, value)
		return value, true
	} else if err == mongo.ErrNoDocuments {
		if InitCheckpoint > ckpt.Timestamp {
			ckpt.Timestamp = InitCheckpoint
		}
		value.Name = ckpt.Name
		value.Timestamp = ckpt.Timestamp
		value.Version = ckpt.Version
		value.OplogDiskQueue = ckpt.OplogDiskQueue
		value.OplogDiskQueueFinishTs = ckpt.OplogDiskQueueFinishTs
		LOG.Info("%s Regenerate checkpoint but won't persist. content: %s", ckpt.Name, value)
		return value, false
	}

	ckpt.close()
	LOG.Warn("%s Reload ckpt find context fail. %v", ckpt.Name, err)
	return nil, false
}

//func (ckpt *MongoCheckpoint) GetMgo() (*CheckpointContext, bool) {
//	if !ckpt.ensureNetwork() {
//		LOG.Warn("%s Reload ckpt ensure network failed. %v", ckpt.Name, ckpt.Conn)
//		return nil, false
//	}
//
//	var err error
//	value := new(CheckpointContext)
//	if err = ckpt.QueryHandle.Find(bson.M{CheckpointName: ckpt.Name}).One(value); err == nil {
//		LOG.Info("%s Load exist checkpoint. content %v", ckpt.Name, value)
//		return value, true
//	} else if err == mgo.ErrNotFound {
//		if InitCheckpoint > ckpt.Timestamp {
//			ckpt.Timestamp = InitCheckpoint
//		}
//		value.Name = ckpt.Name
//		value.Timestamp = ckpt.Timestamp
//		value.Version = ckpt.Version
//		value.OplogDiskQueue = ckpt.OplogDiskQueue
//		value.OplogDiskQueueFinishTs = ckpt.OplogDiskQueueFinishTs
//		LOG.Info("%s Regenerate checkpoint but won't persist. content: %s", ckpt.Name, value)
//		// insert current ckpt snapshot in memory
//		// ckpt.QueryHandle.Insert(value)
//		return value, false
//	}
//
//	ckpt.close()
//	LOG.Warn("%s Reload ckpt find context fail. %v", ckpt.Name, err)
//	return nil, false
//}

func (ckpt *MongoCheckpoint) Insert(updates *CheckpointContext) error {
	if !ckpt.ensureNetwork() {
		LOG.Warn("%s Record ckpt ensure network failed. %v", ckpt.Name, ckpt.client)
		return fmt.Errorf("%s record ckpt network failed", ckpt.Name)
	}

	opts := options.Update().SetUpsert(true)
	filter := bson2.M{CheckpointName: ckpt.Name}
	update := bson2.M{"$set": updates}

	_, err := ckpt.client.Client.Database(ckpt.DB).Collection(ckpt.Table).UpdateOne(nil, filter, update, opts)
	if err != nil {
		LOG.Warn("%s Record checkpoint %v upsert error %v", ckpt.Name, updates, err)
		ckpt.close()
		return err
	}

	LOG.Info("%s Record new checkpoint success [%d]", ckpt.Name,
		int64(utils.ExtractMongoTimestamp(updates.Timestamp)))
	return nil
}

//func (ckpt *MongoCheckpoint) InsertMgo(updates *CheckpointContext) error {
//	if !ckpt.ensureNetwork() {
//		LOG.Warn("%s Record ckpt ensure network failed. %v", ckpt.Name, ckpt.Conn)
//		return fmt.Errorf("%s record ckpt network failed", ckpt.Name)
//	}
//
//	if _, err := ckpt.QueryHandle.Upsert(bson.M{CheckpointName: ckpt.Name}, bson.M{"$set": updates}); err != nil {
//		LOG.Warn("%s Record checkpoint %v upsert error %v", ckpt.Name, updates, err)
//		ckpt.close()
//		return err
//	}
//
//	LOG.Info("%s Record new checkpoint success [%d]", ckpt.Name, int64(utils.ExtractMongoTimestamp(updates.Timestamp)))
//	return nil
//}

// http
type HttpApiCheckpoint struct {
	CheckpointContext

	URL string
}

func (ckpt *HttpApiCheckpoint) Get() (*CheckpointContext, bool) {
	var err error
	var resp *http.Response
	var stream []byte
	value := new(CheckpointContext)
	if resp, err = http.Get(ckpt.URL); err != nil {
		LOG.Warn("%s Http api ckpt request failed, %v", ckpt.Name, err)
		return nil, false
	}

	if stream, err = ioutil.ReadAll(resp.Body); err != nil {
		return nil, false
	}
	if err = json.Unmarshal(stream, value); err != nil {
		return nil, false
	}
	// TODO, may have problem
	if value.Timestamp == 0 {
		// use default start position
		value.Timestamp = ckpt.Timestamp
		value.Name = ckpt.Name
		value.OplogDiskQueueFinishTs = ckpt.OplogDiskQueueFinishTs
		value.OplogDiskQueue = ckpt.OplogDiskQueue
		return value, false
	}
	return value, true
}

func (ckpt *HttpApiCheckpoint) Insert(insert *CheckpointContext) error {
	body, _ := json.Marshal(insert)
	if resp, err := http.Post(ckpt.URL, "application/json", bytes.NewReader(body)); err != nil || resp.StatusCode != http.StatusOK {
		LOG.Warn("%s Context api manager write request failed, %v", ckpt.Name, err)
		return err
	}

	LOG.Info("%s Record new checkpoint success [%d]", ckpt.Name, int64(utils.ExtractMongoTimestamp(insert.Timestamp)))
	return nil
}

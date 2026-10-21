//go:build !nomongo

package extdb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pimpbase/pimpbase/internal/formula"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func init() { register(mongoDriver{}) }

type mongoDriver struct{}

func (mongoDriver) Kind() string        { return "mongodb" }
func (mongoDriver) Label() string       { return "MongoDB" }
func (mongoDriver) DefaultPort() string { return "27017" }

func (mongoDriver) Open(s Settings) (Source, error) {
	uri := s.URI
	if uri == "" {
		if s.Host == "" {
			return nil, errorf("enter the database server's host name or a connection string")
		}
		port := s.Port
		if port == "" {
			port = "27017"
		}
		u := url.URL{Scheme: "mongodb", Host: net.JoinHostPort(s.Host, port), Path: "/"}
		if s.User != "" {
			u.User = url.UserPassword(s.User, s.Password)
		}
		q := url.Values{}
		switch s.SSL {
		case "require":
			q.Set("tls", "true")
			q.Set("tlsInsecure", "true")
		case "verify":
			q.Set("tls", "true")
		}
		for _, o := range s.Options {
			if o.Key != "" {
				q.Set(o.Key, o.Value)
			}
		}
		u.RawQuery = q.Encode()
		uri = u.String()
	}
	db := s.Database
	if db == "" {
		if u, err := url.Parse(uri); err == nil {
			db = strings.Trim(u.Path, "/")
		}
	}
	if db == "" {
		return nil, errorf("name the database to use")
	}
	n := s.MaxConnections
	if n <= 0 {
		n = 5
	}
	opts := options.Client().ApplyURI(uri).SetAppName("pimpbase").SetMaxPoolSize(uint64(n)).
		SetConnectTimeout(10 * time.Second).SetServerSelectionTimeout(10 * time.Second)
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, errorf("%s", Clean(err))
	}
	return &mongoSource{client: client, db: client.Database(db), set: s}, nil
}

type mongoSource struct {
	client *mongo.Client
	db     *mongo.Database
	set    Settings
}

func (m *mongoSource) Close() error { return m.client.Disconnect(context.Background()) }
func (m *mongoSource) Rows() Rows   { return m }

func (m *mongoSource) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return m.client.Ping(ctx, nil)
}

func (m *mongoSource) Tables(ctx context.Context) ([]TableInfo, error) {
	names, err := m.db.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, errorf("reading the collections failed: %s", Clean(err))
	}
	sort.Strings(names)
	out := []TableInfo{}
	for _, n := range names {
		if strings.HasPrefix(n, "system.") {
			continue
		}
		out = append(out, TableInfo{Schema: m.db.Name(), Name: n, Kind: "collection"})
	}
	return out, nil
}

// Describe samples up to 200 documents of a collection for its fields.
func (m *mongoSource) Describe(ctx context.Context, schema, name string) (*TableInfo, error) {
	cur, err := m.db.Collection(name).Find(ctx, bson.D{}, options.Find().SetLimit(200))
	if err != nil {
		return nil, errorf("reading %s failed: %s", name, Clean(err))
	}
	defer cur.Close(ctx)
	kinds := map[string]map[string]bool{}
	var order []string
	for cur.Next(ctx) {
		var doc bson.D
		if err := cur.Decode(&doc); err != nil {
			return nil, errorf("%s", Clean(err))
		}
		for _, e := range doc {
			if kinds[e.Key] == nil {
				kinds[e.Key] = map[string]bool{}
				order = append(order, e.Key)
			}
			if k := bsonKind(e.Value); k != "" {
				kinds[e.Key][k] = true
			}
		}
	}
	t := &TableInfo{Schema: m.db.Name(), Name: name, Kind: "collection", PrimaryKey: []string{"_id"}}
	if kinds["_id"] == nil {
		order = append([]string{"_id"}, order...)
		kinds["_id"] = map[string]bool{"objectid": true}
	}
	for _, n := range order {
		c := Column{Name: n, Type: "text", Nullable: true, DBType: "mixed"}
		if len(kinds[n]) == 1 {
			for k := range kinds[n] {
				c.DBType = k
				switch k {
				case "number":
					c.Type = "number"
				case "bool":
					c.Type = "boolean"
				case "date":
					c.Type = "date"
				case "object", "array":
					c.Type = "longtext"
				case "binary":
					c.Binary = true
				}
			}
		}
		if n == "_id" {
			c.Auto, c.Nullable = true, false
			if c.DBType == "objectid" {
				c.Generated = true
			}
		}
		t.Columns = append(t.Columns, c)
	}
	return t, nil
}

func bsonKind(v any) string {
	switch v.(type) {
	case nil:
		return ""
	case int32, int64, float64, bson.Decimal128:
		return "number"
	case bool:
		return "bool"
	case bson.DateTime, bson.Timestamp:
		return "date"
	case string:
		return "string"
	case bson.ObjectID:
		return "objectid"
	case bson.D, bson.M:
		return "object"
	case bson.A:
		return "array"
	case bson.Binary:
		return "binary"
	}
	return "string"
}

// plain turns decoded BSON into JSON-like Go values.
func plain(v any) any {
	switch x := v.(type) {
	case bson.D:
		out := make(map[string]any, len(x))
		for _, e := range x {
			out[e.Key] = plain(e.Value)
		}
		return out
	case bson.M:
		out := make(map[string]any, len(x))
		for k, y := range x {
			out[k] = plain(y)
		}
		return out
	case bson.A:
		out := make([]any, len(x))
		for i, y := range x {
			out[i] = plain(y)
		}
		return out
	case bson.ObjectID:
		return x.Hex()
	case bson.DateTime:
		return x.Time().UTC()
	case bson.Timestamp:
		return time.Unix(int64(x.T), 0).UTC()
	case bson.Decimal128:
		return x.String()
	case int32:
		return float64(x)
	case int64:
		if x > 1<<53 || x < -(1<<53) {
			return fmt.Sprint(x)
		}
		return float64(x)
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
		return x
	case bson.Binary:
		return base64.StdEncoding.EncodeToString(x.Data)
	case nil, bool, string:
		return x
	}
	return fmt.Sprint(v)
}

// extJSON reads MongoDB extended JSON (such as {"$oid": "…"}) of any shape.
func extJSON(src, what string) (any, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	var wrap bson.D
	if err := bson.UnmarshalExtJSON([]byte(`{"v":`+src+`}`), false, &wrap); err != nil {
		return nil, errorf("the %s is not valid JSON: %s", what, Clean(err))
	}
	return wrap[0].Value, nil
}

func (m *mongoSource) Run(ctx context.Context, q Statement) (*Result, error) {
	if q.Collection == "" {
		return nil, errorf("choose the collection")
	}
	coll := m.db.Collection(q.Collection)
	max := q.MaxRows
	if max <= 0 {
		max = 5000
	}
	filter, err := extJSON(q.Filter, "filter")
	if err != nil {
		return nil, err
	}
	doc, err := extJSON(q.Document, "document")
	if err != nil {
		return nil, err
	}
	var opt struct {
		Sort       json.RawMessage `json:"sort"`
		Projection json.RawMessage `json:"projection"`
		Limit      int64           `json:"limit"`
		Skip       int64           `json:"skip"`
		Upsert     bool            `json:"upsert"`
	}
	if strings.TrimSpace(q.Options) != "" {
		if err := json.Unmarshal([]byte(q.Options), &opt); err != nil {
			return nil, errorf("the options are not valid JSON: %s", err)
		}
	}
	if filter == nil {
		filter = bson.D{}
	}
	writes := map[string]bool{"insertOne": true, "insertMany": true, "updateOne": true, "updateMany": true, "deleteOne": true, "deleteMany": true}
	if writes[q.Operation] && m.set.ReadOnly {
		return nil, ErrReadOnly
	}
	res := &Result{Rows: []map[string]any{}, Query: true}
	read := func(cur *mongo.Cursor) error {
		defer cur.Close(ctx)
		for cur.Next(ctx) {
			if len(res.Rows) >= max {
				res.Truncated = true
				break
			}
			var d bson.D
			if err := cur.Decode(&d); err != nil {
				return err
			}
			res.Rows = append(res.Rows, plain(d).(map[string]any))
		}
		return cur.Err()
	}
	switch q.Operation {
	case "", "find":
		fo := options.Find().SetLimit(int64(max) + 1)
		if opt.Limit > 0 && opt.Limit <= int64(max) {
			fo.SetLimit(opt.Limit)
		}
		if opt.Skip > 0 {
			fo.SetSkip(opt.Skip)
		}
		if len(opt.Sort) > 0 {
			s, err := extJSON(string(opt.Sort), "sort")
			if err != nil {
				return nil, err
			}
			fo.SetSort(s)
		}
		if len(opt.Projection) > 0 {
			p, err := extJSON(string(opt.Projection), "projection")
			if err != nil {
				return nil, err
			}
			fo.SetProjection(p)
		}
		cur, err := coll.Find(ctx, filter, fo)
		if err != nil {
			return nil, errorf("%s", Clean(err))
		}
		if err := read(cur); err != nil {
			return nil, errorf("%s", Clean(err))
		}
	case "findOne":
		var d bson.D
		err := coll.FindOne(ctx, filter).Decode(&d)
		if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errorf("%s", Clean(err))
		}
		if err == nil {
			res.Rows = append(res.Rows, plain(d).(map[string]any))
		}
	case "aggregate":
		pipe, ok := filter.(bson.A)
		if !ok {
			return nil, errorf("an aggregation is a list of stages: [{\"$match\": …}, …]")
		}
		cur, err := coll.Aggregate(ctx, pipe)
		if err != nil {
			return nil, errorf("%s", Clean(err))
		}
		if err := read(cur); err != nil {
			return nil, errorf("%s", Clean(err))
		}
	case "count":
		n, err := coll.CountDocuments(ctx, filter)
		if err != nil {
			return nil, errorf("%s", Clean(err))
		}
		res.Rows = append(res.Rows, map[string]any{"count": float64(n)})
	case "insertOne":
		if doc == nil {
			return nil, errorf("write the document to insert")
		}
		r, err := coll.InsertOne(ctx, doc)
		if err != nil {
			return nil, errorf("%s", Clean(err))
		}
		res.Query, res.RowsAffected = false, 1
		res.Rows = append(res.Rows, map[string]any{"insertedId": plain(r.InsertedID)})
	case "insertMany":
		list, ok := doc.(bson.A)
		if !ok {
			return nil, errorf("insertMany takes a list of documents")
		}
		r, err := coll.InsertMany(ctx, list)
		if err != nil {
			return nil, errorf("%s", Clean(err))
		}
		res.Query, res.RowsAffected = false, int64(len(r.InsertedIDs))
		for _, id := range r.InsertedIDs {
			res.Rows = append(res.Rows, map[string]any{"insertedId": plain(id)})
		}
	case "updateOne", "updateMany":
		if doc == nil {
			return nil, errorf("write the update, such as {\"$set\": {…}}")
		}
		uo := options.UpdateOne().SetUpsert(opt.Upsert)
		var r *mongo.UpdateResult
		if q.Operation == "updateOne" {
			r, err = coll.UpdateOne(ctx, filter, doc, uo)
		} else {
			r, err = coll.UpdateMany(ctx, filter, doc, options.UpdateMany().SetUpsert(opt.Upsert))
		}
		if err != nil {
			return nil, errorf("%s", Clean(err))
		}
		res.Query, res.RowsAffected = false, r.ModifiedCount
		res.Rows = append(res.Rows, map[string]any{"matched": float64(r.MatchedCount), "modified": float64(r.ModifiedCount)})
	case "deleteOne", "deleteMany":
		var r *mongo.DeleteResult
		if q.Operation == "deleteOne" {
			r, err = coll.DeleteOne(ctx, filter)
		} else {
			if d, ok := filter.(bson.D); ok && len(d) == 0 {
				return nil, errorf("deleteMany needs a filter; to empty a collection use {\"_id\": {\"$exists\": true}}")
			}
			r, err = coll.DeleteMany(ctx, filter)
		}
		if err != nil {
			return nil, errorf("%s", Clean(err))
		}
		res.Query, res.RowsAffected = false, r.DeletedCount
		res.Rows = append(res.Rows, map[string]any{"deleted": float64(r.DeletedCount)})
	default:
		return nil, errorf("%q is not a MongoDB operation Pimpbase runs", q.Operation)
	}
	seen := map[string]bool{}
	for _, r := range res.Rows {
		keys := make([]string, 0, len(r))
		for k := range r {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !seen[k] {
				seen[k] = true
				res.Columns = append(res.Columns, k)
			}
		}
	}
	for _, r := range res.Rows {
		for k, v := range r {
			if t, ok := v.(time.Time); ok {
				r[k] = t.Format(formula.DateLayout)
			}
		}
	}
	return res, nil
}

// Rows.

// key converts a row key from the API to the stored _id: an ObjectID when
// it looks like one and the column holds them.
func (m *mongoSource) key(t *Target, k any) any {
	s, ok := k.(string)
	if !ok {
		return k
	}
	if c := t.Column("_id"); c != nil && (c.DBType == "objectid" || c.DBType == "mixed") {
		if id, err := bson.ObjectIDFromHex(s); err == nil {
			return id
		}
	}
	return s
}

func (m *mongoSource) In(t *Target, column string, values []any) *Cond {
	list := bson.A{}
	for _, v := range values {
		if column == "_id" {
			v = m.key(t, v)
		}
		list = append(list, v)
	}
	return &Cond{Mongo: bson.D{{Key: column, Value: bson.D{{Key: "$in", Value: list}}}}}
}

func (m *mongoSource) Count(ctx context.Context, t *Target, where *Cond) (int, error) {
	f := any(bson.D{})
	if where != nil {
		f = where.Mongo
	}
	n, err := m.db.Collection(t.Name).CountDocuments(ctx, f)
	if err != nil {
		return 0, errorf("%s", Clean(err))
	}
	return int(n), nil
}

func (m *mongoSource) Select(ctx context.Context, t *Target, s Select) ([]map[string]any, error) {
	f := any(bson.D{})
	if s.Where != nil {
		f = s.Where.Mongo
	}
	fo := options.Find()
	sortDoc := bson.D{}
	for _, o := range s.Order {
		dir := 1
		if o.Desc {
			dir = -1
		}
		sortDoc = append(sortDoc, bson.E{Key: o.Column, Value: dir})
	}
	if t.IsKey("_id") && (len(s.Order) == 0 || s.Order[len(s.Order)-1].Column != "_id") {
		sortDoc = append(sortDoc, bson.E{Key: "_id", Value: 1})
	}
	fo.SetSort(sortDoc)
	if s.Limit > 0 {
		fo.SetLimit(int64(s.Limit))
	}
	if s.Offset > 0 {
		fo.SetSkip(int64(s.Offset))
	}
	cur, err := m.db.Collection(t.Name).Find(ctx, f, fo)
	if err != nil {
		return nil, errorf("%s", Clean(err))
	}
	defer cur.Close(ctx)
	out := []map[string]any{}
	for cur.Next(ctx) {
		var d bson.D
		if err := cur.Decode(&d); err != nil {
			return nil, errorf("%s", Clean(err))
		}
		doc := plain(d).(map[string]any)
		row := make(map[string]any, len(t.Columns))
		for i := range t.Columns {
			c := &t.Columns[i]
			if v, ok := doc[c.Name]; ok {
				row[c.Name] = Read(v, c)
			} else {
				row[c.Name] = nil
			}
		}
		out = append(out, row)
	}
	return out, cur.Err()
}

func (m *mongoSource) Insert(ctx context.Context, t *Target, values map[string]any) ([]any, error) {
	if m.set.ReadOnly {
		return nil, ErrReadOnly
	}
	doc := bson.D{}
	for _, c := range t.Columns {
		if v, ok := values[c.Name]; ok && !c.Generated {
			if c.Name == "_id" && v == nil {
				continue
			}
			doc = append(doc, bson.E{Key: c.Name, Value: mongoValue(v)})
		}
	}
	r, err := m.db.Collection(t.Name).InsertOne(ctx, doc)
	if err != nil {
		return nil, errorf("%s", Clean(err))
	}
	return []any{plain(r.InsertedID)}, nil
}

func mongoValue(v any) any {
	if s, ok := v.(string); ok && (strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")) {
		if x, err := extJSON(s, "value"); err == nil {
			return x
		}
	}
	return v
}

func (m *mongoSource) Update(ctx context.Context, t *Target, key []any, values map[string]any) error {
	if m.set.ReadOnly {
		return ErrReadOnly
	}
	set, unset := bson.D{}, bson.D{}
	for _, c := range t.Columns {
		if v, ok := values[c.Name]; ok && !c.Generated && c.Name != "_id" {
			if v == nil {
				unset = append(unset, bson.E{Key: c.Name, Value: ""})
			} else {
				set = append(set, bson.E{Key: c.Name, Value: mongoValue(v)})
			}
		}
	}
	upd := bson.D{}
	if len(set) > 0 {
		upd = append(upd, bson.E{Key: "$set", Value: set})
	}
	if len(unset) > 0 {
		upd = append(upd, bson.E{Key: "$unset", Value: unset})
	}
	if len(upd) == 0 {
		return nil
	}
	if len(key) != 1 {
		return ErrNoRow
	}
	r, err := m.db.Collection(t.Name).UpdateOne(ctx, bson.D{{Key: "_id", Value: m.key(t, key[0])}}, upd)
	if err != nil {
		return errorf("%s", Clean(err))
	}
	if r.MatchedCount == 0 {
		return ErrNoRow
	}
	return nil
}

func (m *mongoSource) Delete(ctx context.Context, t *Target, keys [][]any) (int, error) {
	if m.set.ReadOnly {
		return 0, ErrReadOnly
	}
	ks := bson.A{}
	for _, k := range keys {
		if len(k) == 1 {
			ks = append(ks, m.key(t, k[0]))
		}
	}
	r, err := m.db.Collection(t.Name).DeleteMany(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ks}}}})
	if err != nil {
		return 0, errorf("%s", Clean(err))
	}
	return int(r.DeletedCount), nil
}

// Compile turns the common filters into a MongoDB filter: comparisons of a
// field with constants, IN lists, CONTAINS and STARTSWITH (as regular
// expressions), ISBLANK, AND, OR and NOT.
func (m *mongoSource) Compile(t *Target, c *formula.Checked, env formula.Env) (*Cond, bool) {
	mc := &mongoCompiler{m: m, t: t, c: c, env: env}
	f, ok := mc.pred(c.Root)
	if !ok {
		return nil, false
	}
	return &Cond{Mongo: f}, true
}

type mongoCompiler struct {
	m   *mongoSource
	t   *Target
	c   *formula.Checked
	env formula.Env
}

var (
	mongoAll  = bson.D{}
	mongoNone = bson.D{{Key: "_id", Value: bson.D{{Key: "$exists", Value: false}}}}
)

func (mc *mongoCompiler) constant(n formula.Node) (any, bool) {
	if !formula.Constant(n) {
		return nil, false
	}
	v, err := mc.c.EvalNode(n, mc.env)
	return v, err == nil
}

func (mc *mongoCompiler) field(n formula.Node) (*Column, bool) {
	f, ok := n.(*formula.FieldRef)
	if !ok {
		return nil, false
	}
	c := mc.t.Column(f.Name)
	return c, c != nil && !c.Binary
}

// value converts a constant for comparing with a column.
func (mc *mongoCompiler) value(c *Column, v any) any {
	if t, ok := v.(time.Time); ok {
		return bson.NewDateTimeFromTime(t)
	}
	if c.Name == "_id" {
		return mc.m.key(mc.t, v)
	}
	return v
}

func (mc *mongoCompiler) pred(n formula.Node) (any, bool) {
	if v, ok := mc.constant(n); ok {
		if v == true {
			return mongoAll, true
		}
		return mongoNone, true
	}
	switch n := n.(type) {
	case *formula.FieldRef:
		c, ok := mc.field(n)
		if !ok || c.Type != "boolean" {
			return nil, false
		}
		return bson.D{{Key: c.Name, Value: true}}, true
	case *formula.Unary:
		if n.Op != "NOT" {
			return nil, false
		}
		x, ok := mc.pred(n.X)
		if !ok {
			return nil, false
		}
		return bson.D{{Key: "$nor", Value: bson.A{x}}}, true
	case *formula.Binary:
		switch n.Op {
		case "AND", "OR":
			l, ok := mc.pred(n.L)
			if !ok {
				return nil, false
			}
			r, ok := mc.pred(n.R)
			if !ok {
				return nil, false
			}
			return bson.D{{Key: "$" + strings.ToLower(n.Op), Value: bson.A{l, r}}}, true
		case "=", "<>", "<", "<=", ">", ">=":
			return mc.compare(n)
		}
	case *formula.In:
		c, ok := mc.field(n.X)
		xt := mc.c.TypeOf(n.X)
		if !ok || xt != formula.Number && xt != formula.Text {
			return nil, false
		}
		z, _ := zeroValue(xt)
		list, hasZero := bson.A{}, false
		for _, item := range n.List {
			v, ok := mc.constant(item)
			if !ok {
				return nil, false
			}
			if v == nil {
				v = z
			}
			hasZero = hasZero || v == z
			list = append(list, mc.value(c, v))
		}
		if hasZero {
			list = append(list, nil)
		}
		op := "$in"
		if n.Not {
			op = "$nin"
		}
		return bson.D{{Key: c.Name, Value: bson.D{{Key: op, Value: list}}}}, true
	case *formula.Call:
		return mc.call(n)
	}
	return nil, false
}

func (mc *mongoCompiler) call(n *formula.Call) (any, bool) {
	switch n.Name {
	case "AND", "OR":
		list := bson.A{}
		for _, a := range n.Args {
			p, ok := mc.pred(a)
			if !ok {
				return nil, false
			}
			list = append(list, p)
		}
		return bson.D{{Key: "$" + strings.ToLower(n.Name), Value: list}}, true
	case "NOT":
		if len(n.Args) != 1 {
			return nil, false
		}
		p, ok := mc.pred(n.Args[0])
		if !ok {
			return nil, false
		}
		return bson.D{{Key: "$nor", Value: bson.A{p}}}, true
	case "ISBLANK":
		c, ok := mc.field(n.Args[0])
		if !ok {
			return nil, false
		}
		return bson.D{{Key: c.Name, Value: bson.D{{Key: "$in", Value: bson.A{nil, ""}}}}}, true
	case "CONTAINS", "STARTSWITH", "ENDSWITH":
		if len(n.Args) != 2 {
			return nil, false
		}
		v, ok := mc.constant(n.Args[1])
		if !ok {
			return nil, false
		}
		s := formula.ToText(v)
		if s == "" {
			return mongoAll, true
		}
		// Unwrap LOWER(), UPPER() and TEXT() around the field.
		arg, fold, asText := n.Args[0], false, false
		for {
			call, isCall := arg.(*formula.Call)
			if !isCall || len(call.Args) != 1 {
				break
			}
			switch call.Name {
			case "LOWER", "UPPER":
				fold = true
			case "TEXT":
				asText = true
			default:
				return nil, false
			}
			arg = call.Args[0]
		}
		c, ok := mc.field(arg)
		if !ok {
			return nil, false
		}
		if c.Type != "text" && c.Type != "longtext" && c.Type != "single_select" && !(asText && c.Type == "number") {
			return nil, false
		}
		pat := regexp.QuoteMeta(s)
		switch n.Name {
		case "STARTSWITH":
			pat = "^" + pat
		case "ENDSWITH":
			pat += "$"
		}
		flags := ""
		if fold {
			flags = "i"
		}
		if c.Type == "number" {
			return bson.D{{Key: "$expr", Value: bson.D{{Key: "$regexMatch", Value: bson.D{
				{Key: "input", Value: bson.D{{Key: "$toString", Value: "$" + c.Name}}},
				{Key: "regex", Value: pat}, {Key: "options", Value: flags},
			}}}}}, true
		}
		return bson.D{{Key: c.Name, Value: bson.Regex{Pattern: pat, Options: flags}}}, true
	}
	return nil, false
}

func (mc *mongoCompiler) compare(n *formula.Binary) (any, bool) {
	lt, rt := mc.c.TypeOf(n.L), mc.c.TypeOf(n.R)
	if lt == formula.Null || rt == formula.Null {
		return mongoNone, true
	}
	if lt != rt {
		return nil, false
	}
	op := n.Op
	x, k := n.L, n.R
	if formula.Constant(x) {
		x, k, op = k, x, flip(op)
	}
	c, ok := mc.field(x)
	if !ok {
		return nil, false
	}
	v, ok := mc.constant(k)
	if !ok {
		return nil, false
	}
	if lt == formula.Bool {
		b, isBool := v.(bool)
		if op != "=" && op != "<>" || !isBool && v != nil {
			return nil, false
		}
		if b == (op == "=") {
			return bson.D{{Key: c.Name, Value: true}}, true
		}
		return bson.D{{Key: c.Name, Value: bson.D{{Key: "$ne", Value: true}}}}, true
	}
	if lt != formula.Number && lt != formula.Text && lt != formula.Date {
		return nil, false
	}
	ops := map[string]string{"=": "$eq", "<>": "$ne", "<": "$lt", "<=": "$lte", ">": "$gt", ">=": "$gte"}
	nullMatch := false
	if lt == formula.Date {
		if v == nil {
			return mongoNone, true
		}
	} else {
		z, _ := zeroValue(lt)
		if v == nil {
			v = z
		}
		nullMatch = holds(op, z, v)
	}
	base := bson.D{{Key: c.Name, Value: bson.D{{Key: ops[op], Value: mc.value(c, v)}}}}
	isNull := bson.D{{Key: c.Name, Value: nil}}
	switch {
	case op == "<>" && !nullMatch:
		return bson.D{{Key: "$and", Value: bson.A{base, bson.D{{Key: c.Name, Value: bson.D{{Key: "$ne", Value: nil}}}}}}}, true
	case op != "<>" && nullMatch:
		return bson.D{{Key: "$or", Value: bson.A{base, isNull}}}, true
	}
	return base, true
}

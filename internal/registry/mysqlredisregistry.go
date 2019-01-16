package registry

//mysql -h db4free.net -P 3306 -u ocf_dev -p
// use ocf_dev to connect to the specific db
//this^^ is the command to connect mysql client to db4free
//TODO: normalize permissions and published resources?
//TODO: improve performance/transactionyness of DB queries

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/go-redis/redis"
	//TODO I should probably not init my db in a file other than main
	_ "github.com/go-sql-driver/mysql"
)

/*
func main() {
	dbURI := fmt.Sprintf("%s:%s%s%s%s", dbUsername, dbPassword, dbAddress, dbName, dbParameters)
	fmt.Println(dbURI)
	db, err := sql.Open("mysql", dbURI)
	if err != nil {
		log.Fatal(err)
	}
	uuid, _ := GenerateRandomString(10)
	oneTimeToken, err := ProvisionClient(context.TODO(), db, uuid, "2F1W5fnjK1anvsSir6tgLx5h8-pPZzJOaOHFlYi-bSQ=")
	fmt.Println(oneTimeToken)
	err = RegisterClient(context.TODO(), db, uuid, oneTimeToken, "ERC725")
	fmt.Println("err from RegisterClient: ", err)

	randUserName, _ := GenerateRandomString(10)
		userID, err := RegisterUser(randUserName, "google", db)
		if err != nil {
			log.Fatal("err from RegisterUser: ", err)
		}
		token, err := ProvisionMediator(db, userID)
		fmt.Println("token: ", token)
		if err != nil {
			log.Fatal("err from ProvisionMediator ", err)
		}
	defer db.Close()
}

*/

//it should be easy to switch out implementations with postgres or some other system

/*MysqlRedisRegistry implements the registry interface, using mysql for storing long-lasting data (ex: register users/clients/devices and published resources) and
redis for storing ephemeral data (ex: mapping of device-UUID's and the clusterIP of the container that's connected to it) and maybe caching some tokens
*/
type MysqlRedisRegistry struct {
	*sql.DB
	*redis.Client
}

//InitDB connects to the db and creates any tables that may not exist
//TODO: it may not be best practice to have DB initialization in main code instead of init container
//or maybe the new standard is golang wire library?
func InitDB(ctx context.Context, db *sql.DB) (*sql.DB, error) {

	err := createUserTable(ctx, db)
	if err != nil {
		log.Println("err from createUserTable ", err)
	}
	err = createMediatorTable(ctx, db)
	if err != nil {
		log.Println("err from createMediatorTable ", err)
	}
	err = createTokenTable(ctx, db)
	if err != nil {
		log.Println("err from createTokenTable ", err)
	}
	err = createClientTable(ctx, db)
	if err != nil {
		log.Println("err from createClientTable ", err)
	}
	err = createDeviceTable(ctx, db)
	if err != nil {
		log.Println("err from createDeviceTable ", err)
	}
	return db, nil
}

//TODO does the user table require its own tokens for mediator token porvisioning?
//RegisterUser uses email address for account and uses email provider for authProvider
//for blockchain, account is wallet address or pubkey and authProvider is the ticker symbol (ex: "ETH" for ethereum and "BTC" for bitcoin)
//returns the user_id primary key
func (db MysqlRedisRegistry) RegisterUser(username, authProvider string) (string, error) {
	token, err := GenerateRandomString(tokenEntropy)

	_, err = db.Exec("INSERT INTO user (username, authz_provider, token ) VALUES(?,?,?)", username, authProvider, token)
	if err != nil {
		log.Println("err from inside RegisterUser ", err)
		return "", err
	}

	return token, nil
}

//ProvisionMediator uses accessToken which is tied to the OAuth provider, returned string is a mediator token.
//is there a user token?
func (db MysqlRedisRegistry) ProvisionMediator(username, userToken string) (string, error) {
	var userID sql.NullInt64
	mediatorToken, err := GenerateRandomString(tokenEntropy)
	if err != nil {
		return "", err
	}
	err = db.QueryRow("SELECT user_id FROM user WHERE username = ? AND token = ?", username, userToken).Scan(&userID)
	if err != nil {
		return "", err
	}

	_, err = db.Exec("INSERT INTO mediator (user_id,mediator_token) VALUES(?,?)", userID, mediatorToken)
	return mediatorToken, err
}

//ProvisionDevice returns the one-time device access token to be summarily refreshed by the device.
func (db MysqlRedisRegistry) ProvisionDevice(ctx context.Context, deviceUUID, mediatorToken string) (string, error) {
	row := db.QueryRowContext(ctx, "SELECT mediator_id, user_id FROM mediator WHERE mediator_token = ?;", mediatorToken)

	var mediatorID, userID sql.NullInt64

	err := row.Scan(&mediatorID, &userID)
	if err != nil {
		fmt.Println("err from scan: ", err)
		return "", err
	}
	fmt.Println("mediatorID: ", mediatorID, "\nuserID: ", userID)
	if mediatorID.Valid {
		token, err := GenerateRandomString(tokenEntropy)
		if err != nil {
			return "", err
		}
		//TODO these should probably be rolled into one transaction and be atomic. POTENTIAL BUG
		result, err := db.ExecContext(ctx, "INSERT INTO token (access_token) VALUES(?);", token)
		tokenID, err := result.LastInsertId()
		result, err = db.ExecContext(ctx, "INSERT INTO device (user_id, mediator_id , token_id , device_uuid, logged_in) VALUES(?,?,?,?,?);", userID, mediatorID, tokenID, deviceUUID, false)
		return token, err
	}

	return "", errorMediatorTokenNotfound
}

//RegisterDevice handles the UPDATE oic/sec/account request.
//TODO: can I return "unauthorized" as an error?
//mediatedToken is the token returned to mediator when it registers the device
//TODO double check refresh token field to ensure the device hasn't already been registered
func (db MysqlRedisRegistry) RegisterDevice(deviceUUID, mediatedToken string) (accessToken, userID, refreshToken string, expiresIn int, err error) {
	var token, username sql.NullString
	var tokenID sql.NullInt64
	err = db.QueryRowContext(context.TODO(), "SELECT device.token_id, user.username, token.access_token FROM device INNER JOIN token ON device.token_id = token.token_id INNER JOIN user ON device.user_id = user.user_id WHERE device.device_uuid = ?;", deviceUUID).Scan(&tokenID, &username, &token)
	log.Println("deviceUUID: ", deviceUUID, "\nmediatedToken: ", mediatedToken, "\ntokenID: ", tokenID, "\ntoken: ", token)

	if err != nil {
		return "", "", "", 0, err
	}
	if token.String == mediatedToken && token.Valid && username.Valid {
		fmt.Println("token.string is mediated token")
		accessToken, err := GenerateRandomString(tokenEntropy)
		if err != nil {
			return "", "", "", 0, err
		}
		refreshToken, err := GenerateRandomString(tokenEntropy)
		if err != nil {
			return "", "", "", 0, err
		}
		//ttl := time.Now().UTC().Add(time.Second * time.Duration(accessTokenTTL)).Format(time.RFC3339)
		_, err = db.ExecContext(context.TODO(), "UPDATE token SET refresh_token = ?, access_token = ?, expires_in = DATE_ADD( CURRENT_TIMESTAMP(), INTERVAL ? SECOND) WHERE token_id = ?;", refreshToken, accessToken, accessTokenTTL, tokenID)
		fmt.Println("in registerDevice\naccesstoken:", accessToken, "\nusername: ", username.String, "\nrefreshtoken: ", refreshToken, "\nttl: ", accessTokenTTL)
		return accessToken, username.String, refreshToken, accessTokenTTL, err

	} else {
		fmt.Println("token.string isn't a mediated token and/or username doesn't exist")
		return "", "", "", 0, err //TODO i should be returning a useful error like "token not found" although that'd probably be a 403 FORBIDDEN code
	}
}

//DeleteDevice handles the DELETE oic/sec/account request
func (db MysqlRedisRegistry) DeleteDevice(deviceID, accessToken string) error {
	result, err := db.ExecContext(context.TODO(), "DELETE device , token FROM device JOIN token USING(token_id) WHERE device.device_id = ? AND token.access_token = ?;", deviceID, accessToken)
	rowsAffected, err := result.RowsAffected()
	if rowsAffected == 0 {
		return err //TODO: how to distinguish from incorrect token and non-existent ID?
	}
	return err
}

//LookupPrivateIP looks up the IP of the pod that's connected to the device with that UUID
//would returning "",nil work to communicate that there were no errors, but that the device wasn't found?
//TODO probably needs some rewriting or at the very least renaming
func (db MysqlRedisRegistry) LookupPrivateIP(deviceUUID string) (string, error) {
	ip, err := db.Get(deviceUUID).Result()

	if err != nil {
		if err == redis.Nil {
			//TODO have better error handling. caller should be able to tell the difference between non-existent key and other errors
			return "", err
		}
		return "", err
	}

	return ip, nil
}

//ProvisionClient returns one-time client access token to be summarily refreshed by the client
//TODO: handle non-existant mediator tokens (use 403 FOBIDDEN code?)
func (db MysqlRedisRegistry) ProvisionClient(ctx context.Context, clientUUID, mediatorToken string) (string, error) {
	row := db.QueryRowContext(ctx, "SELECT mediator_id, user_id FROM mediator WHERE mediator_token = ?;", mediatorToken)
	var mediatorID, userID sql.NullInt64

	err := row.Scan(&mediatorID, &userID)
	if err != nil {
		return "", err
	}
	fmt.Println("mediatorID: ", mediatorID, "\nuserID: ", userID)
	if mediatorID.Valid {
		token, err := GenerateRandomString(tokenEntropy)
		if err != nil {
			return "", err
		}

		//these should probably be rolled into one transaction and be atomic. POTENTIAL BUG
		result, err := db.ExecContext(ctx, "INSERT INTO token (access_token) VALUES(?);", token)
		tokenID, err := result.LastInsertId()
		//log.Println("tokenID: ", tokenID, "\nuserID: ", userID, "\nmediatorID: ", mediatorID, "\nclientUUID: ", clientUUID)
		result, err = db.ExecContext(ctx, "INSERT INTO client (user_id, mediator_id , token_id , client_uuid) VALUES(?,?,?,?);", userID, mediatorID, tokenID, clientUUID)
		return token, err
	}
	return "", errorMediatorTokenNotfound

}

//RegisterClient handles the UPDATE oic/sec/account request. TODO: can I return "unauthorized" as an error? what about "entry already exists"?
//since the mediatedToken will be overwritten upon registration, it'd be 403 FORBIDDEN
////mediatedToken is the token returned to mediator when it registers the client
func (db MysqlRedisRegistry) RegisterClient(ctx context.Context, userID, clientUUID, mediatedToken, authProvider string) (accessToken, refreshToken, redirectURI string, expiresIn int, err error) {
	var tokenID, userIDNumber sql.NullInt64
	//todo: do I need to include the mediatedToken in the query?
	err = db.QueryRowContext(ctx, "SELECT client.token_id, client.user_id FROM client INNER JOIN token ON client.token_id = token.token_id WHERE client.client_uuid = ? AND token.access_token = ?;", clientUUID, mediatedToken).Scan(&tokenID, &userIDNumber)
	log.Println("clientUUID: ", clientUUID, "\nmediatedToken: ", mediatedToken, "\ntokenID: ", tokenID)

	if err != nil {
		log.Println("in registerClient. no rows were found that matched the token and clientUUID")
		if err == sql.ErrNoRows {
			//todo I should do something special for this. maybe return nil instead of err?
			return "", "", "", 0, err
		}
		return "", "", "", 0, err
	}
	fmt.Println("token.string is mediated token")
	accessToken, err = GenerateRandomString(tokenEntropy)
	if err != nil {
		return "", "", "", 0, err
	}
	refreshToken, err = GenerateRandomString(tokenEntropy)
	if err != nil {
		return "", "", "", 0, err
	}
	//ttl := time.Now().UTC().Add(time.Second * time.Duration(accessTokenTTL)).Format(time.RFC3339)
	_, err = db.ExecContext(ctx, "UPDATE token SET refresh_token = ?, access_token = ?, expires_in = DATE_ADD( CURRENT_TIMESTAMP(), INTERVAL ? SECOND) WHERE token_id = ?", refreshToken, accessToken, accessTokenTTL, tokenID)
	return accessToken, refreshToken, "", accessTokenTTL, err //TODO should I be calculating the remaining accessTokenTTL?

}

//DeleteClient handles the DELETE oic/sec/account request
func (db MysqlRedisRegistry) DeleteClient(ctx context.Context, clientID, accessToken string) error {

	result, err := db.ExecContext(ctx, "DELETE client , token FROM client JOIN token USING(token_id) WHERE client.client_id = ? AND token.access_token = ?;", clientID, accessToken)
	rowsAffected, err := result.RowsAffected()
	if rowsAffected == 0 {
		return err //TODO: how to distinguish from incorrect token and non-existent ID?
	}
	return err
}

//TODO implement this
//TODO figure out how to handle the auth for this.
func (db MysqlRedisRegistry) DeleteUser(ctx context.Context, userID string) error {
	return nil
}

//UpdateSession returns the int which is the access token TTL in seconds. based on UPDATE /oic/sec/session
//TODO verify the access token
//TODO do I need a "logged in" field in my device table or can I leave that up to redis?
func (db MysqlRedisRegistry) UpdateSession(deviceID, userID, accessToken, podAddr string, loggedIn bool) (int, error) {
	//mysql> SELECT UNIX_TIMESTAMP(expires_in) -UNIX_TIMESTAMP(NOW()) TIME FROM token INNER JOIN device ON token.token_id = device.token_id WHERE device.device_uuid = ?;
	row := db.QueryRowContext(context.TODO(), "SELECT UNIX_TIMESTAMP(expires_in) -UNIX_TIMESTAMP(NOW()) TIME FROM token INNER JOIN device ON token.token_id = device.token_id WHERE device.device_uuid = ?;", deviceID)
	var expiresIn sql.NullInt64
	log.Println("in registry.updatesession. deviceID: ", deviceID)
	err := row.Scan(&expiresIn)
	if err != nil {
		fmt.Println("err from scan: ", err)
		return 0, err
	}
	if !expiresIn.Valid {
		fmt.Println("no TTL value found") //TODO chances are if no TTL value was found, the device is provisioned but not registered
		return 0, err
	}
	if loggedIn {
		fmt.Println("in updateSession. about to SET redis db with deviceID: ", deviceID)
		err := db.Set(deviceID, podAddr, time.Hour).Err() //TODO handle response in case it's an error
		if err != nil {
			return 0, err
		}
		return int(expiresIn.Int64), err
	} else {
		err = db.Set(deviceID, unspecifiedAddress, time.Hour*0).Err()
		if err != nil {
			return 0, err
		}
		return int(expiresIn.Int64), err
	}

}

//RefreshToken refreshes the access token and optionally refreshes the refresh token. returns refreshToken, accessToken, accessToken TTL in seconds, error
//based on UDPATE oic/sec/tokenrefresh request
//TODO make it configurable via env vars whether to issue a new refresh token or recycle the old one. making that assumption simplifies the query
//TODO POTENTIAL BUG: can I assume that the refresh token is unique? collission is very low, but it may be a potential bug
// TODO do I need to confirm that a given token is associated with a given user/device ID?
//SELECT user.username, device_uuid,token.refresh_token FROM device INNER JOIN user ON device.user_id = user.user_id INNER JOIN token ON device.token_id = token.token_id;
//^^ preliminary attempts at a query that checks the device/user ID's
//TODO just break it out into 2 smaller queries
func (db MysqlRedisRegistry) RefreshToken(deviceID, userID, refreshToken string) (accessToken string, returnedRefreshToken string, ttl int, err error) {
	accessToken, err = GenerateRandomString(tokenEntropy)
	if err != nil {
		log.Println("error trying to generate a new refresh token")
		return "", "", 0, err
	}

	result, err := db.ExecContext(context.TODO(), `UPDATE token SET access_token = ?, expires_in = DATE_ADD( CURRENT_TIMESTAMP(), INTERVAL ? SECOND) WHERE refresh_token = ?;`, //TODO: verify validity of deviceID and userID in relation to tokens
		accessToken, accessTokenTTL, refreshToken)
	numAffectedRows, err := result.RowsAffected()
	if err != nil {
		log.Println("err in mysqlregistry.RefreshToken(): ", err)
		return "", "", 0, err
	}
	if numAffectedRows == 0 {
		return "", "", 0, nil
	}
	return accessToken, refreshToken, accessTokenTTL, nil
}

/*IF EXISTS (SELECT user.username, token.token_id, device.device_uuid,token.refresh_token
FROM device INNER JOIN user ON device.user_id = user.user_id
INNER JOIN token ON device.token_id = token.token_id WHERE device_uuid = ? AND user.username = ?)

^^this is a portion of the refreshtoken() query that I removed pending debugging
*/

//SET  @token_id =(SELECT token.token_id FROM device INNER JOIN user ON device.user_id = user.user_id INNER JOIN token ON device.token_id = token.token_id WHERE device_uuid = "device-test-uuid" AND user.username = "MW1VqsF1oPLIKw==");
//TODO: implement RETRIEVE/UPDATE oic/rd

//TODO: implement RETRIEVE oic/res/{device_UUID} gotta make sure this is how you discover the resources. maybe it's {device_UUID}/oic/res

//TODO confirm this is the correct function signature
//TODO should i check the result from ExecContext or leave it blank?
func (db MysqlRedisRegistry) PublishResource(json, deviceID string) error {
	result, err := db.ExecContext(context.TODO(), `update device set published_resources =  ? where device.device_uuid = ?;`, json, deviceID)
	num, err := result.RowsAffected()
	if num == 0 {
		log.Println("zero rows affected by resource publication request")
	}
	return err
}

//this query should be relevant: SELECT JSON_CONTAINS(@j1, '["oic.r.switch.binary"]', '$.links[0].rt');
//how do I loop over every link in the query?
/*
these queries will need modification, but hopefully point in the right direction
SET @j1 = (SELECT published_resources from device);
select json_extract(@j1, '$.links[*].rt');
select * from device where json_extract(@j1, '$.links[*].rt') like '%"oic.r.switch.binary"%';
select json_extract((select published_resources from device),'$.links[*].rt');

is it inefficient to call json_extract twice instead of saving the result?
select * from device where (json_extract(@j1, '$.links[*].rt') like '%"oic.r.switch.binar"%' OR json_extract(@j1, '$.links[*].rt') like '%"oic.r.switch.binary"%');

set @j2 = (select json_extract((select published_resources from device),'$.links[*].rt'));
db.Query(`select published_resources from device inner join user on user.user_id = device.user_id where user.username = ?;` , userID)

maybe use the IN keyword instead of LIKE?

*/

//TODO implement this
func (db MysqlRedisRegistry) FindDevice(userID string, params url.Values) (string, error) {
	//break out params into separate arrays based on field
	//build sql query that includes all params
	//anchor, href, rt, if, endpoints, policy/bitmask
	//(I don't think I should implement anything but rt initially

	//ifParams, ifOK := params["if"]
	//anchorParams, anchorOK := params["anchor"]
	//hrefParams, hrefOK := params["href"]

	//rtParams, rtOK := params["rt"]
	links := "[]"[:]
	var tempStr string
	//TODO fully parse this stuff
	rows, err := db.Query(`select published_resources from device inner join user on user.user_id = device.user_id where user.username = ?;`, userID)
	if err != nil {
		return "", err
	}
	//This loop is a hot mess. gotta fix it
	//good inspiration: https://play.golang.org/p/9JZqzccu8Y5
	for rows.Next() {
		err := rows.Scan(&tempStr)
		links = links[:len(links)-2] + "," + tempStr + links[len(links)-1:]
		if err != nil {
			return "", err
		}
	}
	//iterate through rows, create a JSON payload then return that JSON

	return "", nil
}

//TODO implement adding the user token collumn
func createUserTable(ctx context.Context, db *sql.DB) error {
	fmt.Println("in createUserTable")
	stmt, err := db.Prepare("CREATE TABLE IF NOT EXISTS user( user_id bigint unsigned NOT NULL AUTO_INCREMENT, joinDate datetime NOT NULL DEFAULT NOW(), authz_provider varchar(45) , username varchar(45) NOT NULL ,token varchar(45),PRIMARY KEY (user_id),UNIQUE KEY Ind_58 (username)) AUTO_INCREMENT=1 ;")
	if err != nil {
		return err
	}
	_, err = stmt.Exec()

	return err
}

func createMediatorTable(ctx context.Context, db *sql.DB) error {
	log.Println("creating mediator table")
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS  mediator(
	mediator_id    bigint unsigned NOT NULL AUTO_INCREMENT,
	user_id        bigint unsigned NOT NULL ,
	permission     json ,
	mediator_token varchar(45) NOT NULL ,
	PRIMARY KEY (mediator_id),
	UNIQUE KEY (mediator_token),
	KEY fkIdx_9 (user_id),
	CONSTRAINT FK_9 FOREIGN KEY fkIdx_9 (user_id) REFERENCES user (user_id)
	) AUTO_INCREMENT=1;
	`)
	if err != nil {
		log.Fatal("couldn't create mediator table because: ", err)
	}
	return err
}

func createTokenTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
	CREATE TABLE IF NOT EXISTS token(
	 token_id      bigint unsigned NOT NULL AUTO_INCREMENT ,
	 refresh_token varchar(45) ,
	 access_token  varchar(45) NOT NULL ,
	 expires_in    datetime ,
	PRIMARY KEY (token_id),
	KEY access_token_index (access_token),
	KEY refresh_token_index (refresh_token)
	) AUTO_INCREMENT=1;
	`)
	return err
}

// GenerateRandomString returns a URL-safe, base64 encoded
// securely generated random string.
// It will return an error if the system's secure random
// number generator fails to function correctly, in which
// case the caller should not continue.
func GenerateRandomString(s int) (string, error) {
	b := make([]byte, s)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), err
}

func createClientTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS client
(
 client_id   bigint unsigned NOT NULL AUTO_INCREMENT ,
 user_id    bigint unsigned NOT NULL ,
 mediator_id bigint unsigned NOT NULL ,
 token_id    bigint unsigned NOT NULL ,
 client_uuid char(36) NOT NULL ,
PRIMARY KEY (client_id),
KEY client_uuid_index (client_uuid),
KEY fkIdx_18 (user_id),
CONSTRAINT FK_18 FOREIGN KEY fkIdx_18 (user_id) REFERENCES user (user_id),
KEY fkIdx_50 (mediator_id),
CONSTRAINT FK_50 FOREIGN KEY fkIdx_50 (mediator_id) REFERENCES mediator (mediator_id),
KEY fkIdx_69 (token_id),
CONSTRAINT FK_69 FOREIGN KEY fkIdx_69 (token_id) REFERENCES token (token_id)
) AUTO_INCREMENT=1;
`)
	return err
}

func createDeviceTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS device
		(
		 device_id           bigint unsigned NOT NULL AUTO_INCREMENT ,
		 user_id             bigint unsigned NOT NULL ,
		 published_resources json ,
		 logged_in           tinyint unsigned NOT NULL ,
		 mediator_id         bigint unsigned NOT NULL ,
		 token_id            bigint unsigned NOT NULL ,
		 device_uuid         char(36) NOT NULL ,
		PRIMARY KEY (device_id),
		KEY device_uuid_index (device_uuid),
		KEY fkIdx_26 (user_id),
		CONSTRAINT FK_26 FOREIGN KEY fkIdx_26 (user_id) REFERENCES user (user_id),
		KEY fkIdx_53 (mediator_id),
		CONSTRAINT FK_53 FOREIGN KEY fkIdx_53 (mediator_id) REFERENCES mediator (mediator_id),
		KEY fkIdx_66 (token_id),
		CONSTRAINT FK_66 FOREIGN KEY fkIdx_66 (token_id) REFERENCES token (token_id)
		) AUTO_INCREMENT=1;
		`)
	return err
}

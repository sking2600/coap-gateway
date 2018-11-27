package main

//mysql -h db4free.net -P 3306 -u ocf_dev -p
//this^^ is the command to connect mysql client to db4free
//TODO: normalize permissions and published resources?
//TODO: improve performance/transactionyness of DB queries

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis"
	_ "github.com/go-sql-driver/mysql"
)

var (
	errorMediatorTokenNotfound = errors.New("mediator token not found")
	unspecifiedAddress         = "::/128"
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
type registry interface {
	RegisterUser(username, authProvider string) (uint64, error)
	ProvisionMediator(userID uint64) (string, error)
	ProvisionDevice(ctx context.Context, deviceUUID, mediatorToken string) (string, error)
	RegisterDevice(deviceUUID, mediatedToken string) (accessToken, userID, refreshToken string, expiresIn int, err error)
	DeleteDevice(deviceID, accessToken string) error
	ProvisionClient(ctx context.Context, clientUUID, mediatorToken string) (string, error)
	RegisterClient(ctx context.Context, clientUUID, mediatedToken, authProvider string) error
	DeleteClient(ctx context.Context, clientID, accessToken string) error
	UpdateSession(deviceID, userID, accessToken string, loggedIn bool) (int, error)
	RefreshToken(deviceID, userID, refreshToken string) (string, string, int, error)
}

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

//RegisterUser uses email address for account and uses email provider for authProvider
//for blockchain, account is wallet address or pubkey and authProvider is the ticker symbol (ex: "ETH" for ethereum and "BTC" for bitcoin)
//returns the user_id primary key
func (db MysqlRedisRegistry) RegisterUser(username, authProvider string) (uint64, error) {

	result, err := db.Exec("INSERT INTO user (username, authz_provider ) VALUES(?,?)", username, authProvider)
	if err != nil {
		log.Println("err from inside RegisterUser ", err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		log.Println("err from inside RegisterUser ", err)
	}

	return uint64(userID), nil
}

//ProvisionMediator uses accessToken which is tied to the OAuth provider, returned string is a mediator token.
//is there a user token?
func (db MysqlRedisRegistry) ProvisionMediator(userID uint64) (string, error) {
	token, err := GenerateRandomString(tokenEntropy)
	if err != nil {
		return "", err
	}
	_, err = db.Exec("INSERT INTO mediator (user_id,mediator_token) VALUES(?,?)", userID, token)
	return token, err
}

//ProvisionDevice returns the one-time device access token to be summarily refreshed by the device. TODO
func (db MysqlRedisRegistry) ProvisionDevice(ctx context.Context, deviceUUID, mediatorToken string) (string, error) {
	row := db.QueryRowContext(context.TODO(), "SELECT mediator_id, user_id FROM mediator WHERE mediator_token = ?;", mediatorToken)

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
		//these should probably be rolled into one transaction and be atomic. POTENTIAL BUG
		result, err := db.ExecContext(ctx, "INSERT INTO token (access_token) VALUES(?);", token)
		tokenID, err := result.LastInsertId()
		result, err = db.ExecContext(ctx, "INSERT INTO device (user_id, mediator_id , token_id , device_uuid, logged_in) VALUES(?,?,?,?,?);", userID, mediatorID, tokenID, deviceUUID, false)
		return token, err
	}

	return "", errorMediatorTokenNotfound
}

//RegisterDevice handles the UPDATE oic/sec/account request. TODO: can I return "unauthorized" as an error?
//mediatedToken is the token returned to mediator when it registers the device
//TODO double check refresh token field to ensure the device hasn't already been registered
func (db MysqlRedisRegistry) RegisterDevice(deviceUUID, mediatedToken string) (accessToken, userID, refreshToken string, expiresIn int, err error) {
	var token sql.NullString
	var tokenID, userIDNumber sql.NullInt64
	err = db.QueryRowContext(context.TODO(), "SELECT device.token_id, device.user_id, token.access_token FROM device INNER JOIN token ON device.token_id = token.token_id WHERE device.device_uuid = ?;", deviceUUID).Scan(&tokenID, &userIDNumber, &token)
	log.Println("deviceUUID: ", deviceUUID, "\nmediatedToken: ", mediatedToken, "\ntokenID: ", tokenID, "\ntoken: ", token)

	if err != nil {
		return "", "", "", 0, err
	}
	if token.String == mediatedToken && token.Valid {
		fmt.Println("token.string is mediated token")
		accessToken, err := GenerateRandomString(tokenEntropy)
		if err != nil {
			return "", "", "", 0, err
		}
		refreshToken, err := GenerateRandomString(tokenEntropy)
		if err != nil {
			return "", "", "", 0, err
		}
		ttl := time.Now().UTC().Add(time.Second * time.Duration(accessTokenTTL)).Format(time.RFC3339)
		_, err = db.ExecContext(context.TODO(), "UPDATE token SET refresh_token = ?, access_token = ?, expires_in = DATE_ADD( CURRENT_TIMESTAMP(), INTERVAL ? SECOND) WHERE token_id = ?;", refreshToken, accessToken, ttl, tokenID)
		return accessToken, "TODO GET USER ID", refreshToken, accessTokenTTL, err //TODO return actual values. make sure I'm returning the right refreshToken (might have the wrong scope) should I just return a default value for TTL or actually count the time elapsed?

	} else {
		fmt.Println("token.string isn't mediated token")
		return "", "", "", 0, err //TODO i should be returning a useful error like "token not found" although that'd probably be a 403 FORBIDDEN code
	}
}

//DeleteDevice handles the DELETE oic/sec/account request TODO
func (db MysqlRedisRegistry) DeleteDevice(deviceID, accessToken string) error {
	return nil
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
//TODO this needs to return more stuff
func (db MysqlRedisRegistry) RegisterClient(ctx context.Context, clientUUID, mediatedToken, authProvider string) error {
	var token sql.NullString
	var tokenID, userIDNumber sql.NullInt64
	err := db.QueryRowContext(ctx, "SELECT client.token_id, client.user_id , token.access_token FROM client INNER JOIN token ON client.token_id = token.token_id WHERE client.client_uuid = ?;", clientUUID).Scan(&tokenID, &userIDNumber, &token)
	log.Println("clientUUID: ", clientUUID, "\nmediatedToken: ", mediatedToken, "\ntokenID: ", tokenID, "\ntoken: ", token)

	if err != nil {
		return err
	}
	if token.String == mediatedToken && token.Valid {
		fmt.Println("token.string is mediated token")
		accessToken, err := GenerateRandomString(tokenEntropy)
		if err != nil {
			return err
		}
		refreshToken, err := GenerateRandomString(tokenEntropy)
		if err != nil {
			return err
		}
		ttl := time.Now().UTC().Add(time.Second * time.Duration(accessTokenTTL)).Format(time.RFC3339)
		_, err = db.ExecContext(ctx, "UPDATE token SET refresh_token = ?, access_token = ?, expires_in = DATE_ADD( CURRENT_TIMESTAMP(), INTERVAL ? SECOND) WHERE token_id = ?", refreshToken, accessToken, ttl, tokenID)
		return err

	} else {
		fmt.Println("token.string isn't mediated token")
		return err //TODO i should be returning a useful error like "token not found" although that'd probably be a 403 FORBIDDEN code
	}
}

//DeleteClient handles the DELETE oic/sec/account request TODO
func (db MysqlRedisRegistry) DeleteClient(ctx context.Context, clientID, accessToken string) error {

	result, err := db.ExecContext(ctx, "DELETE client , token FROM client JOIN token USING(token_id) WHERE client.client_id = ? AND token.access_token = ?;", clientID, accessToken)
	rowsAffected, err := result.RowsAffected()
	if rowsAffected == 0 {
		return err //TODO: how to distinguish from incorrect token and non-existent ID?
	}
	return err
}

//UpdateSession returns the uint which is the access token TTL in seconds. based on UPDATE /oic/sec/session
//TODO
func (db MysqlRedisRegistry) UpdateSession(deviceID, userID, accessToken string, loggedIn bool) (int, error) {
	//mysql> SELECT UNIX_TIMESTAMP(expires_in) -UNIX_TIMESTAMP(NOW()) TIME FROM token INNER JOIN device ON token.token_id = device.token_id WHERE device.device_uuid = ?;
	row := db.QueryRowContext(context.TODO(), "SELECT UNIX_TIMESTAMP(expires_in) -UNIX_TIMESTAMP(NOW()) TIME FROM token INNER JOIN device ON token.token_id = device.token_id WHERE device.device_uuid = ?;", deviceID)
	var expiresIn sql.NullInt64

	err := row.Scan(&expiresIn)
	if err != nil {
		fmt.Println("err from scan: ", err)
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
//TODO can I assume that the refresh token is unique? collission is very low, but it may be a latent bug
//I may need to denormalize my databases to make the queries easier
//TODO figure out how to pull this  whole thing off in one transaction
//TODO do it in 2 transactions for the time being
func (db MysqlRedisRegistry) RefreshToken(deviceID, userID, refreshToken string) (string, string, int, error) {
	accessToken, err := GenerateRandomString(tokenEntropy)
	if err != nil {
		log.Println("error trying to generate a new refresh token")
		return "", "", 0, err
	}
	result, err := db.ExecContext(context.TODO(), `IF EXISTS (SELECT user.username, token.token_id, device.device_uuid,token.refresh_token 
		FROM device INNER JOIN user ON device.user_id = user.user_id 
		INNER JOIN token ON device.token_id = token.token_id WHERE device_uuid = ? AND user.username = ?)

		UPDATE token SET access_token = ? WHERE refresh_token = ?;`, //if I could save the token.token_id from the first query, I may be able to improve the update
		deviceID, userID, accessToken, refreshToken)
	fmt.Println(result)
	return "", "", 0, nil
}

//SET  @token_id =(SELECT token.token_id FROM device INNER JOIN user ON device.user_id = user.user_id INNER JOIN token ON device.token_id = token.token_id WHERE device_uuid = "device-test-uuid" AND user.username = "MW1VqsF1oPLIKw==");
//TODO: implement RETRIEVE/UPDATE oic/rd

//TODO: implement RETRIEVE oic/res/{device_UUID} gotta make sure this is how you discover the resources. maybe it's {device_UUID}/oic/res

//TODO: this isn't actually searching on username
func findUser(username string, db *sql.DB) error {
	fmt.Println("in findUser")
	results, err := db.Query("SELECT * FROM user;")
	defer results.Close()
	for results.Next() {
		var authProvider, username, time string
		var userid uint64

		err = results.Scan(&userid, &time, &authProvider, &username)
		fmt.Println(userid, time, authProvider, username)
	}
	return err
}

func deleteUserTable(db *sql.DB) error {
	stmt, err := db.Prepare("DROP TABLE user ;")
	_, _ = stmt.Exec()
	return err
}

func createUserTable(ctx context.Context, db *sql.DB) error {
	fmt.Println("in createUserTable")
	stmt, err := db.Prepare("CREATE TABLE IF NOT EXISTS user( user_id bigint unsigned NOT NULL AUTO_INCREMENT, joinDate datetime NOT NULL DEFAULT NOW(), authz_provider varchar(45) , username varchar(45) NOT NULL ,PRIMARY KEY (user_id),UNIQUE KEY Ind_58 (username)) AUTO_INCREMENT=1 ;")
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

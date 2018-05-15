'use strict';

const express = require('express');
const fs = require('fs');
const fetch = require('node-fetch');

const app = express();

const api_key = "EOOEMOW4YR6QNB07";

const Datastore = require('@google-cloud/datastore');
const datastore = Datastore();

async function getSession() {
	const sessionKey = datastore.key(['Session', 'default']);
	
	const sessions = await datastore.get(sessionKey);
	if (sessions[0]) { 
		// TODO handle session expiry
		return sessions[0];
	}

	const session = await login();
	session.device_serial = await getDevice(session);
	
	await datastore.upsert({key: sessionKey, data: session});
	return session;
}

async function login() {
	const secrets = JSON.parse(fs.readFileSync(__dirname + "/secrets.json"));

	const response = await fetch('https://support.iaqualink.com/users/sign_in.json', {
		body: JSON.stringify({
			api_key: api_key, 
			email: secrets.email, 
			password: secrets.password }),
		headers: {
			'content-type': 'application/json'},
		method: 'POST'
	});
	if (response.status !== 200)
		throw new Error('sign_in.json failure - status:' + response.status);
	const json = await response.json();
	if (!('session_id' in json) || !('id' in json) || !('authentication_token' in json)) {
		console.error('Unexpected signin response: ', json);
		throw new Error('sign_in.json failure - unexpected response');
	}
	const s = {
		id: json.session_id,
		user_id: json.id,
		authentication_token: json.authentication_token};
	console.log('logged in with session', s);
	return s;
}

async function getDevice(session) {
	const url = 'https://support.iaqualink.com/devices.json' + 
		'?api_key=' + api_key +
		'&authentication_token=' + session.authentication_token +
		'&user_id=' + session.user_id;
	const response = await fetch(url);
	if (response.status !== 200) {
		const body = await response.text();
		console.error('devices.json failure', url, response.status, response.statusText, body);
		throw new Error('devices.json failure:' + response.status + ' ' + response.statusText);
	}
	const json = await response.json();
	if (!('0' in json) || !('serial_number' in json[0])) {
		console.error('Unexpected devices response: ', json);
		throw new Error('devices.json failure - unexpected response');
	}
	return json[0].serial_number;
}

async function getTemps(session) {
	const url = 'https://iaqualink-api.realtime.io/v1/mobile/session.json' +
		'?actionID=command' +
		'&command=get_home' +
		'&serial=' + session.device_serial +
		'&sessionID=' + session.id;
		const response = await fetch(url);
	if (response.status !== 200) {
		const body = await response.text();
		console.error('session.json failure', url, response.status, response.statusText, body);
		throw new Error('session.json failure:' + response.status + ' ' + response.statusText);
	}
	const json = await response.json();
	
	// Convert array of key/value pairs into an object
	const items = Object.assign({}, ...json.home_screen);
	
	// Compute heater temperature.
	// "1" means heating, "3" means on but not heating
	// "spa" (temp 1) seems to take precedence when it's on
	let heater = 0;
	if (items.spa_heater==="1")
		heater = parseInt(items.spa_set_point, 10);
	else if(items.pool_heater==="1")
		heater = parseInt(items.pool_set_point, 10);
		
	return {
		air: parseInt(items.air_temp, 10),
		pool: parseInt(items.pool_temp, 10),
		heater: heater};
}

async function update() {
	const session = await getSession();
	const temps = await getTemps(session);
	
	// Get the most recent database entry
	const latestTempsKey = datastore.key(['Temps', 'latest']);
	const results = await datastore.get(latestTempsKey);	
	if (results[0]) {
		const lt = results[0];
		if (temps.air === lt.air && temps.pool === lt.pool && temps.heater === lt.heater)
			return 'No change: ' + JSON.stringify(temps);
	}
	await datastore.upsert({key: latestTempsKey, data: temps});
	
	// Append the new entry with a timestamp
	temps.timestamp = new Date();
	await datastore.save({key: datastore.key(['Temps']), data: temps});

	return 'Added entry: ' + JSON.stringify(temps);
}

async function log(response) {
	const query = datastore.createQuery('Temps')
    	.order('timestamp', { descending: true });
	const results = await datastore.runQuery(query);
	
	// Create the response in a string.
	// If this gets really big we could setup a streaming response,
	// and use a query cursor.
	let csv = 'timesamp, air, pool, heater\n';
    for(let temps of results[0])
    	csv += `${temps.timestamp.toISOString()}, ${temps.air}, ${temps.pool}, ${temps.heater}\n`;
	
	response
		.status(200)
        .set('Content-Type', 'text/csv')
     	.send(csv)
     	.end();
}

app.get('/', (req, res) => {
	res.sendFile(__dirname + "/static/index.html");
});

app.get('/update', (req, res) => {
	update().then(msg => {
		res.status(200).send(msg).end();
	}).catch(error => {
		console.error(error);
		res.status(500).send('Server error!');
	});
});

app.get('/log.csv', (req, res) => {
	log(res).catch(error => {
		console.error(error);
		res.status(500).send('Server error!');
	});
});


// Start the server
const PORT = process.env.PORT || 8080;
app.listen(PORT, () => {
  console.log(`App listening on port ${PORT}`);
  console.log('Press Ctrl+C to quit.');
});
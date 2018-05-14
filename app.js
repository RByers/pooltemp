'use strict';

const express = require('express');
const fs = require('fs');
const fetch = require('node-fetch');

const app = express();

const api_key = "EOOEMOW4YR6QNB07";
var secrets = JSON.parse(fs.readFileSync(__dirname + "/secrets.json"));

var user_id;
var session_id;
var authentication_token;
var device_serial;

async function login() {
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
	session_id = json.session_id;
	user_id = json.id;
	authentication_token = json.authentication_token;
	console.log('logged in with session', session_id);
	return session_id;
}

async function getDevice() {
	var url = 'https://support.iaqualink.com/devices.json' + 
		'?api_key=' + api_key +
		'&authentication_token=' + authentication_token +
		'&user_id=' + user_id;
	const response = await fetch(url);
	if (response.status !== 200) {
		var body = await response.text();
		console.error('devices.json failure', url, response.status, response.statusText, body);
		throw new Error('devices.json failure:' + response.status + ' ' + response.statusText);
	}
	const json = await response.json();
	if (!('0' in json) || !('serial_number' in json[0])) {
		console.error('Unexpected devices response: ', json);
		throw new Error('devices.json failure - unexpected response');
	}
	device_serial = json[0].serial_number;
	return device_serial;
}

async function getTemps() {
	var url = 'https://iaqualink-api.realtime.io/v1/mobile/session.json' +
		'?actionID=command' +
		'&command=get_home' +
		'&serial=' + device_serial +
		'&sessionID=' + session_id;
		const response = await fetch(url);
	if (response.status !== 200) {
		var body = await response.text();
		console.error('session.json failure', url, response.status, response.statusText, body);
		throw new Error('session.json failure:' + response.status + ' ' + response.statusText);
	}
	const json = await response.json();
	
	// Convert array of key/value pairs into an object
	var items = Object.assign({}, ...json.home_screen);
	
	// Compute heater temperature.
	// "1" means heating, "3" means on but not heating
	// "spa" (temp 1) seems to take precedence when it's on
	var heater = 0;
	if (items.spa_heater==="1")
		heater = parseInt(items.spa_set_point, 10);
	else if(items.pool_heater==="1")
		heater = parseInt(items.pool_set_point, 10);
		
	return {
		air: parseInt(items.air_temp, 10),
		pool: parseInt(items.pool_temp, 10),
		heater: heater};
}

app.get('/', (req, res) => {
	res.sendFile(__dirname + "/static/index.html");
});

app.post('/live', (req, res) => {
	login().then(getDevice).then(getTemps).then(temps => {
		res.status(200).send('Got temps: ' + JSON.stringify(temps)).end();
	}).catch(error => {
		console.error(error);
		res.status(500).send('Server error: ' + error);
	});
});

// Start the server
const PORT = process.env.PORT || 8080;
app.listen(PORT, () => {
  console.log(`App listening on port ${PORT}`);
  console.log('Press Ctrl+C to quit.');
});
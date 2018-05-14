'use strict';

const express = require('express');
const request = require('request');
const fs = require('fs');

const app = express();

const api_key = "EOOEMOW4YR6QNB07";
var secrets = JSON.parse(fs.readFileSync(__dirname + "/secrets.json"));

var user_id;
var session_id;
var authentication_token;
var device_serial;

function login() {
	return new Promise(function(resolve, reject) {
		request.post(
	    	'https://support.iaqualink.com/users/sign_in.json',
	    	{ json: {api_key: api_key, email: secrets.email, password: secrets.password }},
	    	function (error, response, body) {
	    		if(error) {
	    			console.log("sign_in.json error", error);
	    			reject(error);
    			} else if (response.statusCode !== 200 || 
    					!('session_id' in body) || !('id' in body) || !('authentication_token' in body)) {
    				console.log("sign_in.json failure", response.statusCode, body);
    				reject("sign_in.json failure");
    			} else {
	        		session_id = body.session_id;
	        		user_id = body.id;
	        		authentication_token = body.authentication_token;
	        		resolve(session_id);
	        	}
	        }
		);
	});
}

function getDevice() {
	return new Promise(function(resolve, reject) {
		request.get({
			url: "https://support.iaqualink.com//devices.json",
			json: true,
			qs: {
				api_key: api_key,
				authentication_token: authentication_token,
				user_id: user_id}},
			function(error, response, body) {
	    		if(error) {
	    			console.log("devices.json error", error);
	    			reject(error);
    			} else if (response.statusCode !== 200 || !('0' in body) || !('serial_number' in body[0])) {
    				console.log("devices.json failure", response.statusCode, body);
    				reject(body);
				} else {
					device_serial = body[0].serial_number;
					resolve(device_serial);
				}
			});
		});
}

function getTemps() {
	return new Promise(function(resolve, reject) {
		request.get({
			url: "https://iaqualink-api.realtime.io/v1/mobile/session.json",
			json: true,
			qs: {
				actionID: "command",
				command: "get_home",
				serial: device_serial,
				sessionID: session_id}},
			function(error, response, body) {
	    		if(error) {
	    			console.log("session.json error", error);
	    			reject(error);
    			} else if (response.statusCode !== 200 || !('home_screen' in body)) {
    				console.log("session.json failure", response.statusCode, body);
    				reject(body);
				} else {
					// Convert array of key/value pairs into an object
					var items = Object.assign({}, ...body.home_screen);
					
					// Compute heater temperature.
					// "1" means heating, "3" means on but not heating
					// "spa" (temp 1) seems to take precedence when it's on
					var heater = 0;
					if (items.spa_heater==="1")
						heater = parseInt(items.spa_set_point, 10);
					else if(items.pool_heater==="1")
						heater = parseInt(items.pool_set_point, 10);
						
					resolve({
						air: parseInt(items.air_temp, 10),
						pool: parseInt(items.pool_temp, 10),
						heater: heater});
				}
			});
		});	
}

app.get('/', (req, res) => {
	res.sendFile(__dirname + "/static/index.html");
});

app.post('/live', (req, res) => {
	login().then(getDevice).then(getTemps).then(temps => {
		res.status(200).send('Got temps: ' + JSON.stringify(temps)).end();
	}).catch(error => {
		res.status(500).send('Server error: ' + error);
	});
});

// Start the server
const PORT = process.env.PORT || 8080;
app.listen(PORT, () => {
  console.log(`App listening on port ${PORT}`);
  console.log('Press Ctrl+C to quit.');
});
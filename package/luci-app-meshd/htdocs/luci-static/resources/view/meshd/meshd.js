'use strict';
'require view';

/*
 * Hosts the meshd PWA inside LuCI. The PWA is embedded in the meshd binary and
 * served on the daemon's HTTP port (default 8080) on the same host, so we frame
 * it from there. This is the "LuCI app first" step: the rpcd `meshd` object
 * (see /usr/libexec/rpcd/meshd) and its ACL are in place so a future
 * LuCI-native view — or the PWA itself — can call meshd through an
 * authenticated ubus session once the management API moves behind localhost.
 */
return view.extend({
	// Pure embed view: no config to load or save.
	load: function() { return Promise.resolve(); },
	handleSaveApply: null,
	handleSave: null,
	handleReset: null,

	render: function() {
		var src = window.location.protocol + '//' + window.location.hostname + ':8080/';
		return E('iframe', {
			'src': src,
			'style': 'width:100%;height:80vh;border:0;background:#fff'
		});
	}
});

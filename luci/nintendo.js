'use strict';
'require view';
'require rpc';
'require ui';
'require poll';

var getStatus = rpc.declare({ object: 'nintendo-control', method: 'status', nobatch: true, expect: { '': {} } });
var setConfig = rpc.declare({ object: 'nintendo-control', method: 'set', nobatch: true, params: [ 'devices', 'upstream' ], expect: { '': {} } });
var labels = { off: '关闭拦截', updates: '只拦截系统更新', nintendo: '只拦截任天堂服务（保留其他网络）', all: '禁止该设备全部外网' };

return view.extend({
	load: function() { return getStatus(); },
	render: function(data) {
		var config = data.config || {}, rows = [], limit = data.max_devices || 64;
		var upstream = E('input', { 'class': 'cbi-input-text', value: config.upstream || '127.0.0.1:53', 'aria-label': '上游 DNS' });
		var state = E('div', { 'style': 'padding:1em;border:1px solid #aaa;border-radius:6px;margin:1em 0;' });
		var counters = E('p'), recent = E('div'), domainList = E('div'), nintendoList = E('div');
		var deviceTable = E('table', { 'class': 'table' });
		var button, addButton, saving = false;
		var renderRows = function() {
			deviceTable.replaceChildren(E('tr', { 'class': 'tr table-titles' }, [
				E('th', {}, '名称'), E('th', {}, 'MAC 地址'), E('th', {}, '拦截等级'), E('th', {}, '操作')
			]));
			rows.forEach(function(row) { deviceTable.appendChild(row.node); });
			if (!rows.length) deviceTable.appendChild(E('tr', { 'class': 'tr' }, E('td', { colspan: 4 }, '暂无设备。添加主机并保存后开始控制。')));
			if (addButton) addButton.disabled = saving || rows.length >= limit;
		};
		var addRow = function(d) {
			if (rows.length >= limit) return;
			var row = {};
			row.name = E('input', { 'class': 'cbi-input-text', value: d.name || '', maxlength: 64, placeholder: '例如：客厅 Switch 2', 'aria-label': '设备名称' });
			row.mac = E('input', { 'class': 'cbi-input-text', value: d.mac || '', maxlength: 17, placeholder: '02:aa:bb:cc:dd:01', 'aria-label': '设备 MAC 地址' });
			row.mode = E('select', { 'class': 'cbi-input-select', 'aria-label': '拦截等级' }, Object.keys(labels).map(function(k) {
				return E('option', { value: k, selected: (d.mode || 'updates') === k ? '' : null }, labels[k]);
			}));
			row.remove = E('button', { 'class': 'cbi-button cbi-button-remove', click: function() {
				rows = rows.filter(function(r) { return r !== row; }); renderRows();
			} }, '移除');
			row.node = E('tr', { 'class': 'tr' }, [E('td', {}, row.name), E('td', {}, row.mac), E('td', {}, row.mode), E('td', {}, row.remove)]);
			rows.push(row); renderRows();
		};
		(config.devices || []).forEach(addRow);
		var draw = function(s) {
			var healthy = s.running && s.firewall_ok, devices = s.devices || [], names = {};
			var active = devices.filter(function(d) { return d.mode !== 'off'; }).length;
			devices.forEach(function(d) { names[d.mac] = d.name || d.mac; });
			state.replaceChildren(E('strong', {}, healthy ? ('已生效：' + devices.length + ' 台设备，' + active + ' 台启用拦截') : '保护状态异常，请检查服务'));
			devices.forEach(function(d) {
				state.appendChild(E('div', {}, (d.name || '未命名设备') + ' · ' + d.mac + ' · ' + (labels[d.mode] || d.mode) + ' · IP：' + ((d.ips || []).join(', ') || '尚未发现')));
			});
			state.appendChild(E('div', {}, '检查时间：' + new Date().toLocaleTimeString()));
			state.style.borderColor = healthy ? '#3b8f50' : '#c00';
			if (s.error) state.appendChild(E('p', { 'style': 'color:#b00020' }, s.error));
			counters.textContent = '本次服务启动以来（全部设备合计）：DNS 拦截 ' + (s.dns_blocked || 0) + ' 次 · HTTPS/HTTP 拦截 ' + (s.https_blocked || 0) + ' 次 · 放行连接 ' + (s.allowed || 0) + ' 次 · 当前连接 ' + (s.active_connections || 0);
			var events = (s.recent || []).slice().reverse();
			recent.replaceChildren(events.length ? E('table', { 'class': 'table' }, [
				E('tr', { 'class': 'tr table-titles' }, [ E('th', {}, '时间'), E('th', {}, '设备 / 来源'), E('th', {}, '类型'), E('th', {}, '域名') ])
			].concat(events.map(function(e) { return E('tr', { 'class': 'tr' }, [ E('td', {}, new Date(e.time).toLocaleString()), E('td', {}, (names[e.mac] ? names[e.mac] + ' · ' : '') + (e.client || '')), E('td', {}, e.protocol), E('td', { 'style': 'word-break:break-all' }, e.host) ]); }))) : E('p', {}, '暂无拦截记录。无需在主机上主动检查更新来测试。'));
			domainList.replaceChildren(E('ul', {}, (s.update_domains || []).map(function(d) { return E('li', {}, E('code', {}, d)); })));
			nintendoList.replaceChildren(
				E('p', {}, '仅精确匹配的联网检测域名由路由器本地响应：' + (s.connection_test_domains || []).join(', ')),
				E('p', {}, '精确透传到真实服务器的测速、IPv6 检测和 HTTPS API：' + (s.pass_through_test_domains || []).join(', ')),
				E('ul', {}, (s.nintendo_domains || []).map(function(d) { return E('li', {}, E('code', {}, d)); })));
		};
		var setBusy = function(busy) {
			saving = busy; button.disabled = busy; addButton.disabled = busy || rows.length >= limit; upstream.disabled = busy;
			rows.forEach(function(r) { [r.name, r.mac, r.mode, r.remove].forEach(function(el) { el.disabled = busy; }); });
		};
		addButton = E('button', { 'class': 'cbi-button cbi-button-add', click: function() { addRow({}); } }, '添加设备');
		button = E('button', { 'class': 'cbi-button cbi-button-apply', click: function() {
			var devices = rows.map(function(r) { return { name: r.name.value.trim(), mac: r.mac.value.trim().toLowerCase(), mode: r.mode.value }; });
			var seen = {};
			for (var i = 0; i < devices.length; i++) {
				var d = devices[i];
				if (!/^(?:[0-9a-f]{2}:){5}[0-9a-f]{2}$/.test(d.mac) || d.mac === '00:00:00:00:00:00' || (parseInt(d.mac.slice(0, 2), 16) & 1)) {
					ui.addNotification(null, E('p', {}, '第 ' + (i + 1) + ' 台设备：请输入有效的单播 MAC 地址。'), 'error'); return;
				}
				if (seen[d.mac]) { ui.addNotification(null, E('p', {}, 'MAC 地址重复：' + d.mac), 'error'); return; }
				seen[d.mac] = true;
			}
			setBusy(true);
			return setConfig(devices, upstream.value.trim()).then(function(s) {
				draw(s);
				if (s.error || !s.running || !s.firewall_ok) throw new Error(s.error || '规则尚未生效');
				ui.addNotification(null, E('p', {}, '设备列表已保存并生效。'), 'info');
			}).catch(function(e) { ui.addNotification(null, E('p', {}, '应用失败：' + e.message), 'error'); })
			.finally(function() { setBusy(false); });
		} }, '保存并立即应用');
		renderRows(); draw(data);
		poll.add(function() { return getStatus().then(draw).catch(function() {
			state.replaceChildren(E('strong', {}, '无法读取服务状态，请刷新页面或检查路由器服务。'));
			state.style.borderColor = '#c00';
		}); }, 5);
		return E('div', {}, [
			E('h2', {}, 'Nintendo 控制'),
			E('p', {}, '按 MAC 独立控制多台 Switch / Switch 2，最多 ' + limit + ' 台。设置持久保存，重启后自动恢复。Wi-Fi 和有线网卡请分别添加。'),
			state,
			E('div', { 'class': 'cbi-section' }, [
				E('div', { 'style': 'overflow-x:auto' }, deviceTable), addButton,
				E('p', {}, '添加、移除或切换等级后请保存。移除设备会解除其拦截；只重建策略发生变化的设备连接。'),
				E('div', { 'class': 'cbi-value' }, [ E('label', { 'class': 'cbi-value-title' }, '上游 DNS'), E('div', { 'class': 'cbi-value-field' }, [ upstream, E('div', { 'class': 'cbi-value-description' }, '全部设备共用。通常保持 127.0.0.1:53；不能指向本程序的 5354 / 5355 / 5356 端口。') ]) ]),
				button
			]),
			E('ul', {}, [
				E('li', {}, [ E('strong', {}, '只拦截系统更新：'), '拦截已知更新检查、固件下载和最低系统版本查询（sun / atumn / aqua），保留游戏 CDN（atum）。部分游戏或 DLC 的版本检查仍可能报错。' ]),
				E('li', {}, [ E('strong', {}, '只拦截任天堂服务：'), '参考 90DNS 拦截任天堂商店、联机、更新和遥测。Switch 1/2 的 HTTP 联网检测本地返回正确响应；精确放行 NintendoClients 文档列出的测速和 HTTPS API（真实连接 Nintendo），并保留其他普通网络。IPv6 检测需要路由器本身有 IPv6；不会伪造 HTTPS 证书。' ]),
				E('li', {}, [ E('strong', {}, '禁止该设备全部外网：'), '禁止该设备经路由器访问互联网及其他网段，所有域名和第三方游戏联网都会停止；同一局域网内的设备直连不受此转发规则影响。旧版 all 模式保留这一行为。' ]),
				E('li', {}, '启用时会阻止该设备的 IPv6 转发、QUIC 和加密 DNS 专用端口，拒绝无法识别域名的 HTTPS/HTTP 连接。'),
				E('li', {}, '第一档按已知域名拦截，未来新增域名可能需要补充。两档均无法控制主机已下载的固件、卡带更新或改连其他网络。')
			]),
			E('h3', {}, '最近拦截'), counters, recent,
			E('details', {}, [ E('summary', {}, '系统更新域名清单'), domainList ]),
			E('details', {}, [ E('summary', {}, '任天堂服务域名清单（参考 90DNS）'), nintendoList ])
		]);
	},
	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});

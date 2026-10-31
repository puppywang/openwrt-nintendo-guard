'use strict';
'require view';
'require rpc';
'require ui';
'require poll';

var getStatus = rpc.declare({ object: 'nintendo-control', method: 'status', nobatch: true, expect: { '': {} } });
var setConfig = rpc.declare({ object: 'nintendo-control', method: 'set', nobatch: true, params: [ 'mode', 'mac', 'upstream' ], expect: { '': {} } });
var labels = { off: '关闭拦截', updates: '只拦截系统更新', all: '拦截全部任天堂服务' };

return view.extend({
	load: function() { return getStatus(); },
	render: function(data) {
		var config = data.config || {};
		var mode = E('select', { 'class': 'cbi-input-select' }, Object.keys(labels).map(function(k) {
			return E('option', { value: k, selected: config.mode === k ? '' : null }, labels[k]);
		}));
		var mac = E('input', { 'class': 'cbi-input-text', value: config.mac || '', maxlength: 17, placeholder: '02:aa:bb:cc:dd:01', 'aria-label': '设备 MAC 地址' });
		var upstream = E('input', { 'class': 'cbi-input-text', value: config.upstream || '127.0.0.1:53', 'aria-label': '上游 DNS' });
		var state = E('div', { 'style': 'padding:1em;border:1px solid #aaa;border-radius:6px;margin:1em 0;' });
		var counters = E('p');
		var recent = E('div');
		var domainList = E('div');
		var button;
		var draw = function(s) {
			var c = s.config || {}, healthy = s.running && s.firewall_ok;
			state.replaceChildren(E('strong', {}, healthy ? ('已生效：' + (labels[c.mode] || c.mode)) : '保护状态异常，请检查服务'),
				E('div', {}, '设备：' + (c.mac || '未知') + '　IP：' + ((s.device_ips || []).join(', ') || '尚未发现')),
				E('div', {}, '检查时间：' + new Date().toLocaleTimeString()));
			state.style.borderColor = healthy ? '#3b8f50' : '#c00';
			if (s.error) state.appendChild(E('p', { 'style': 'color:#b00020' }, s.error));
			counters.textContent = '本次服务启动以来：DNS 拦截 ' + (s.dns_blocked || 0) + ' 次 · HTTPS/HTTP 拦截 ' + (s.https_blocked || 0) + ' 次 · 放行连接 ' + (s.allowed || 0) + ' 次 · 当前连接 ' + (s.active_connections || 0);
			var events = (s.recent || []).slice().reverse();
			recent.replaceChildren(events.length ? E('table', { 'class': 'table' }, [
				E('tr', { 'class': 'tr table-titles' }, [ E('th', {}, '时间'), E('th', {}, '来源'), E('th', {}, '类型'), E('th', {}, '域名') ])
			].concat(events.map(function(e) { return E('tr', { 'class': 'tr' }, [ E('td', {}, new Date(e.time).toLocaleString()), E('td', {}, e.client || ''), E('td', {}, e.protocol), E('td', { 'style': 'word-break:break-all' }, e.host) ]); }))) : E('p', {}, '暂无拦截记录。无需在主机上主动检查更新来测试。'));
			domainList.replaceChildren(E('ul', {}, (s.update_domains || []).map(function(d) { return E('li', {}, E('code', {}, d)); })));
		};
		button = E('button', { 'class': 'cbi-button cbi-button-apply', click: function() {
			if (!(mode.value === 'off' && mac.value.trim() === '') && !/^(?:[0-9a-f]{2}:){5}[0-9a-f]{2}$/i.test(mac.value.trim())) {
				ui.addNotification(null, E('p', {}, '请输入有效的 MAC 地址，例如 02:aa:bb:cc:dd:01。'), 'error'); return;
			}
			button.disabled = true;
			return setConfig(mode.value, mac.value.trim(), upstream.value.trim()).then(function(s) {
				draw(s);
				if (s.error || !s.running || !s.firewall_ok) throw new Error(s.error || '规则尚未生效');
				ui.addNotification(null, E('p', {}, '已保存并生效：' + labels[s.config.mode]), 'info');
			}).catch(function(e) { ui.addNotification(null, E('p', {}, '应用失败：' + e.message), 'error'); })
			.finally(function() { button.disabled = false; });
		} }, '保存并立即应用');
		draw(data);
		poll.add(function() { return getStatus().then(draw).catch(function() {
			state.replaceChildren(E('strong', {}, '无法读取服务状态，请刷新页面或检查路由器服务。'));
			state.style.borderColor = '#c00';
		}); }, 5);
		return E('div', {}, [
			E('h2', {}, 'Nintendo 控制'),
			E('p', {}, '仅作用于下方 MAC 地址对应的设备。设置会持久保存，路由器重启后自动恢复。'),
			state,
			E('div', { 'class': 'cbi-section' }, [
				E('div', { 'class': 'cbi-value' }, [ E('label', { 'class': 'cbi-value-title' }, '拦截等级'), E('div', { 'class': 'cbi-value-field' }, mode) ]),
				E('div', { 'class': 'cbi-value' }, [ E('label', { 'class': 'cbi-value-title' }, 'Switch 2 MAC 地址'), E('div', { 'class': 'cbi-value-field' }, mac) ]),
				E('div', { 'class': 'cbi-value' }, [ E('label', { 'class': 'cbi-value-title' }, '上游 DNS'), E('div', { 'class': 'cbi-value-field' }, [ upstream, E('div', { 'class': 'cbi-value-description' }, '通常保持 127.0.0.1:53；使用其他本机 DNS 服务时填写其 IP:端口。不能指向本程序的 5354 端口。') ]) ]),
				E('p', {}, '切换等级或更换设备时，会重建该设备的相关连接；其他设备不受影响。'),
				button
			]),
			E('ul', {}, [
				E('li', {}, [ E('strong', {}, '只拦截系统更新：'), '拦截已知的系统更新检查、固件下载和最低系统版本查询（sun / atumn / aqua）；保留游戏 CDN（atum）。版本检查被拦截时，部分游戏或 DLC 下载仍可能报错。' ]),
				E('li', {}, [ E('strong', {}, '拦截全部任天堂服务（断开外网）：'), '禁止这台 Switch 经路由器访问互联网及其他网段，覆盖缓存 IP 和已有联机连接。商店、下载、云存档以及第三方游戏联网都会停止；同一局域网内的设备直连不受此转发规则影响。' ]),
				E('li', {}, '启用任一等级时，检查 DNS 和 HTTPS/HTTP 域名；禁用该设备的 IPv6、QUIC 和加密 DNS 专用端口，拒绝无法识别域名的 HTTPS/HTTP 连接。'),
				E('li', {}, '第一档按已知域名拦截，未来新增域名可能需要补充。两档均无法控制主机已下载的固件、卡带更新或改连其他网络。')
			]),
			E('h3', {}, '最近拦截'), counters, recent,
			E('details', {}, [ E('summary', {}, '系统更新域名清单'), domainList ])
		]);
	},
	handleSaveApply: null,
	handleSave: null,
	handleReset: null
});

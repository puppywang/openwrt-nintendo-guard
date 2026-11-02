// Exercise the LuCI form with a small DOM adapter and the real view module.
'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function E(tag, attrs = {}, children = []) {
    const node = { tag, attrs, style: {}, value: attrs.value || '', disabled: false, children: [],
        appendChild(child) { this.children.push(child); },
        replaceChildren(...items) { this.children = items; }
    };
    node.children = (Array.isArray(children) ? children : [children]).filter(x => x != null);
    if (tag === 'select') {
        const option = node.children.find(x => x.attrs.selected != null) || node.children[0];
        node.value = option ? option.value : '';
    }
    return node;
}
function descendants(node, predicate) {
    if (!node || typeof node !== 'object') return [];
    return (predicate(node) ? [node] : []).concat(...(node.children || []).map(n => descendants(n, predicate)));
}

async function main() {
    let status = { running: true, firewall_ok: true, max_devices: 64,
        config: { upstream: '127.0.0.1:53', devices: [
            {name:'Switch 2',mac:'02:aa:bb:cc:dd:01',mode:'updates'},
            {name:'Switch',mac:'02:aa:bb:cc:dd:02',mode:'all'},
            {name:'Off',mac:'02:aa:bb:cc:dd:03',mode:'off'}
        ] }
    };
    status.devices = status.config.devices;
    let poll, submitted = [], notifications = [];
    const context = vm.createContext({ E, view: {extend: x => x},
        rpc: {declare: spec => spec.method === 'status' ? () => Promise.resolve(status) : (devices, upstream) => {
            assert.deepEqual(Array.from(spec.params), ['devices','upstream']);
            const config = JSON.parse(JSON.stringify({devices,upstream}));
            submitted.push(config);
            status = {...status, config, devices:config.devices};
            return Promise.resolve(status);
        }},
        ui: {addNotification: (...args) => notifications.push(args)},
        poll: {add: fn => { poll = fn; }}
    });
    const source = fs.readFileSync(path.join(__dirname,'../luci/nintendo.js'),'utf8');
    const view = vm.runInContext('(function(){'+source+'})()',context);
    const root = view.render(status);
    const controls = label => descendants(root,n => n.attrs['aria-label'] === label);
    const button = text => descendants(root,n => n.tag === 'button' && n.children[0] === text)[0];
    const rows = () => descendants(root,n => n.tag === 'tr' && n.children[0]?.children?.[0]?.tag === 'input');
    assert.equal(rows().length,3);
    controls('设备名称')[0].value = 'Edited';
    await poll();
    assert.equal(controls('设备名称')[0].value,'Edited','poll overwrote unsaved edits');
    await button('保存并立即应用').attrs.click();
    assert.deepEqual(submitted[0].devices.map(d=>d.mode),['updates','all','off']);
    assert.equal(submitted[0].devices[0].name,'Edited');
    button('添加设备').attrs.click();
    controls('设备 MAC 地址')[3].value = '02:AA:BB:CC:DD:01';
    await button('保存并立即应用').attrs.click();
    assert.equal(submitted.length,1,'duplicate MAC reached backend');
    assert.equal(notifications.at(-1)[2],'error');
    controls('设备 MAC 地址')[3].value = 'ff:ff:ff:ff:ff:ff';
    await button('保存并立即应用').attrs.click();
    assert.equal(submitted.length,1,'multicast MAC reached backend');
    controls('设备 MAC 地址')[3].value = '02:aa:bb:cc:dd:04';
    controls('拦截等级')[3].value = 'all';
    await button('保存并立即应用').attrs.click();
    assert.equal(submitted[1].devices.length,4);
    assert.equal(submitted[1].devices[3].mode,'all');
    while (rows().length) descendants(root,n=>n.tag==='button' && n.children[0]==='移除')[0].attrs.click();
    await button('保存并立即应用').attrs.click();
    assert.deepEqual(submitted[2].devices,[],'empty list was not saved');
    assert.equal(button('保存并立即应用').disabled,false);
    console.log('PASS LuCI: independent modes, add/remove, validation, polling preserves edits, empty list');
}
main().catch(err => { console.error(err); process.exitCode=1; });

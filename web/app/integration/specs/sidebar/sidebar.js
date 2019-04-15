const assert = require('assert');
const fetch = require('node-fetch');
describe('linkerd dashboard', () => {
    it('should have the right title', () => {
        browser.url('http://127.0.0.1:7777');
        browser.pause(5000);
        sidebar= $$("[role=menu]");
        const menu = sidebar[0];
        list=menu.$$("a").map(item=>item.getText());
        assert.equal(list[0], 'Overview');
        assert.equal(list[1], 'Tap');
        assert.equal(list[2], 'Top');
        assert.equal(list[3], 'Top Routes');
        assert.equal(list[4], 'Service Mesh');
        assert.equal(list[5],"Debug");
        fin_list=sidebar[1].$$("[role=menuitem]").map(item=>item.getText());
        assert.equal(fin_list[0],"Documentation");
        ind=fin_list[1].search("Community");
        assert.notEqual(-1,ind);
        assert.equal(fin_list[2],"Join the Mailing List");
        assert.equal(fin_list[3],"Join us on Slack");
        assert.equal(fin_list[4],"File an Issue");
        const resources = menu.$("li");
        assert.equal(resources.getText(), 'Resources');
        resources.click();
        browser.pause(3000);
        sidebar= $$("[role=menu]");
        const sub_menu=sidebar[1];
        list=sub_menu.$$("[role=menuitem]");
        assert.equal(list[0].getText(), 'Authorities');
        assert.equal(list[1].getText(), 'Deployments');
        assert.equal(list[2].getText(), 'Daemon Sets');
        assert.equal(list[3].getText(), 'Jobs');
        assert.equal(list[4].getText(), 'Namespaces');
        assert.equal(list[5].getText(), 'Pods');
        assert.equal(list[6].getText(), 'Replication Controllers');
        assert.equal(list[7].getText(), 'Stateful Sets');
        for(let item of list){
             item.scrollIntoView();
             item.click();
             browser.pause(3000);
             item_sub_menu=item.$(function(){return this.nextSibling;});
             ind=item_sub_menu.$$('div')[0].getText().search("All");
             console.log("ind value is: "+ind)
             assert.notEqual(-1,ind);
             item.click();
             browser.pause(1000);
        }
        resources.click();
}
)});
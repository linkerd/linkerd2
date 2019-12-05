const assert = require('assert');

describe('url detection and routing test', function() {
  it('should detect namespace in URL and display correct namespace', () => {
    browser.url(global.dashboardAddress + '/namespaces/linkerd/pods');
    browser.waitUntil(() => {
      let buttonArray = browser.react$$('MenuList Button');
      let namespaceSelectionButton = buttonArray.find(button => {
        return button.getText() === "LINKERD";
      })
      return namespaceSelectionButton
    }, 1000, 'timed out while waiting for namespace selection button');
  });
  it('clicking logo on top left should redirect to namespaces view', () => {
    // logo is the first svg rendered in the dashboard
    const linkerdWordLogo = browser.react$('div a svg');
    linkerdWordLogo.click();
    const currentUrl = browser.getUrl();
    assert.equal(currentUrl, global.dashboardAddress + '/namespaces');
  });
});

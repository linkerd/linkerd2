const assert = require('assert');
describe('logo link test', function() {
  it('should redirect to the home view if logo is clicked', () => {
    browser.url('http://localhost:7777/tap');
    const link = $('.linkerd-word-logo');
    link.click();
    const breadcrumbHeader = $('.breadcrumb-link');
    const pageTitle = breadcrumbHeader.getText();
    assert.equal(pageTitle, 'Overview');
  });
});

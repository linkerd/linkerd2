const assert = require('assert');
let namespaceSelectionButton, namespaceOptions, newNamespaceOption;

describe('namespace selection test', function() {
  it('should identify a namespace selection button titled `default`', () => {
    browser.url(global.dashboardAddress);
    menuButtons = browser.react$$('MenuList Button');
    namespaceSelectionButton = menuButtons.find(button => {
      return button.getText() === "DEFAULT";
    });
  })
  it('should open a menu when namespace selection button is clicked', () => {
    namespaceSelectionButton.click();
    browser.waitUntil(() => {
      namespaceOptions = browser.react$$('MenuList Menu MenuItem');
      return namespaceOptions.length > 0;
    }, 1000, 'timed out while waiting for namespace options to display');
  });
  it('should click on a namespace option', () => {
    newNamespaceOption = namespaceOptions.find(namespace => {
      return namespace.getText() === "linkerd";
    });
    newNamespaceOption.click();
  });
  it('namespace selection button should display new namespace option', () => {
    const newNamespace = namespaceSelectionButton.getText().toLowerCase();
    assert.equal(newNamespace, "linkerd");
  });
  it('should confirm namespace selection menu is closed', () => {
    browser.waitUntil(() => {
      namespaceOptions = browser
        .react$$('MenuList Menu MenuItem')
        .find(namespaceOption => {
          return namespaceOption.getText() === "All Namespaces";
      });
      return !namespaceOptions;
    }, 1000, 'timed out while waiting for namespace options to disappear');
  });
  it('should click on the deployments list view', () => {
    const resourceOptions = browser.react$$('MenuList MenuItem ListItem');
    const deploymentResource = resourceOptions.find(resource => {
      return resource.getText() === "Deployments";
    });
    deploymentResource.click();
  });
  it('breadcrumb header text should display correct text', () => {
    const breadcrumbHeader = browser.react$$('BreadcrumbHeader');
    const breadcrumbText = breadcrumbHeader.reduce((acc, crumb) => {
      acc+= crumb.getText();
      return acc;
      }, '');
    assert.equal(breadcrumbText, "Namespace >linkerd >Deployment");
  });
  it('should display a list of resources', () => {
    browser.waitUntil(() => {
      return browser
        .react$$('MetricsTable BaseTable TableBody TableRow a')
        .length > 0;
    }, 1000, 'timed out while waiting for resource list');
  });
  it('should click on a resource detail page', () => {
    const resourceList = browser
      .react$$('MetricsTable BaseTable TableBody TableRow a');
    const resourceDetailLink = resourceList.find(tableRowLink => {
      return tableRowLink.getText() === "linkerd-controller";
    });
    resourceDetailLink.click();
  });
  it('should select a different namespace from a resource detail page', () => {
    namespaceSelectionButton.click();
    namespaceOptions = browser.react$$('MenuList Menu MenuItem');
    newNamespaceOption = namespaceOptions.find(namespace => {
      return namespace.getText() === "default";
    })
    newNamespaceOption.click();
  })
  it('should cancel the modal confirmation dialog', () => {
    let cancelNamespaceChangeButton = browser
      .react$$('NamespaceConfirmationModal Button')
      .find(button => {
        return button.getText() === "NO";
      });
    cancelNamespaceChangeButton.click();
  });
  it('should close the confirmation dialog', () => {
    browser.waitUntil(() => {
      let modal = browser.react$('NamespaceConfirmationModal');
      return !modal.isDisplayed();
    }, 1000, 'timed out while waiting to close the confirmation dialog');
  });
  it('should select a different namespace', () => {
    namespaceSelectionButton.click();
    namespaceOptions = browser.react$$('MenuList Menu MenuItem');
    newNamespaceOption = namespaceOptions.find(namespace => {
      return namespace.getText() === "default";
    });
    newNamespaceOption.click();
  });
  it('should accept the modal confirmation dialog', () => {
    let acceptNamespaceChangeButton = browser
      .react$$('NamespaceConfirmationModal Button').find(button => {
        return button.getText() === "YES";
    });
    acceptNamespaceChangeButton.click();
  });
  it('should navigate to the namespace detail page for new namespace', () => {
    const breadcrumbHeader = browser.react$$('BreadcrumbHeader');
    const breadcrumbText = breadcrumbHeader.reduce((acc, crumb) => {
      acc = acc + crumb.getText();
      return acc;
      }, '')
    assert.equal(breadcrumbText, "Namespace >default");
  });
})

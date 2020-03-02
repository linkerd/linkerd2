import ApiHelpers from './util/ApiHelpers.jsx';
import GrafanaLink from './GrafanaLink.jsx';
import { routerWrap } from '../../test/testHelpers.jsx';
import { mount } from 'enzyme';

describe('GrafanaLink', () => {
  it('makes a link', () => {
    let api = ApiHelpers('');
    let linkProps = {
      resource: "replicationcontroller",
      name: "aldksf-3409823049823",
      namespace: "myns",
      PrefixedLink: api.PrefixedLink
    };
    let component = mount(routerWrap(GrafanaLink, linkProps));

    let expectedDashboardNameStr = "/linkerd-replicationcontroller";
    let expectedNsStr = "var-namespace=myns";
    let expectedVarNameStr = "var-replicationcontroller=aldksf-3409823049823";

    expect(component.find("GrafanaLink")).toHaveLength(1);

    const href = component.find('a').props().href;

    expect(href).toContain(expectedDashboardNameStr);
    expect(href).toContain(expectedNsStr);
    expect(href).toContain(expectedVarNameStr);
  });

  it('makes a link without a namespace', () => {
    let api = ApiHelpers('');
    let linkProps = {
      resource: "replicationcontroller",
      name: "aldksf-3409823049823",
      PrefixedLink: api.PrefixedLink
    };
    let component = mount(routerWrap(GrafanaLink, linkProps));

    let expectedDashboardNameStr = "/linkerd-replicationcontroller";
    let expectedVarNameStr = "var-replicationcontroller=aldksf-3409823049823";

    expect(component.find("GrafanaLink")).toHaveLength(1);

    const href = component.find('a').props().href;

    expect(href).toContain(expectedDashboardNameStr);
    expect(href).not.toContain("namespace");
    expect(href).toContain(expectedVarNameStr);
  });
});

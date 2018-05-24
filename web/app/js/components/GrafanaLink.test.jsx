import { ApiHelpers } from './util/ApiHelpers.jsx';
import GrafanaLink from './GrafanaLink.jsx';
import { mount } from 'enzyme';
import { routerWrap } from '../test/helpers.jsx';

describe('GrafanaLink', () => {
  it('makes a link', () => {
    let api = ApiHelpers('');
    let linkProps = {
      resource: "Replication Controller",
      name: "aldksf-3409823049823",
      namespace: "myns",
      conduitLink: api.ConduitLink
    };
    let component = mount(routerWrap(GrafanaLink, linkProps));

    let expectedDashboardNameStr = "/conduit-replication-controller";
    let expectedNsStr = "var-namespace=myns";
    let expectedVarNameStr = "var-replication_controller=aldksf-3409823049823";

    expect(component.find("GrafanaLink")).toHaveLength(1);

    const href = component.find('a').props().href;

    expect(href).toContain(expectedDashboardNameStr);
    expect(href).toContain(expectedNsStr);
    expect(href).toContain(expectedVarNameStr);
  });
});

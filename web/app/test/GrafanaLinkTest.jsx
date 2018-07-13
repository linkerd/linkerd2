import Adapter from 'enzyme-adapter-react-16';
import ApiHelpers from '../js/components/util/ApiHelpers.jsx';
import { expect } from 'chai';
import GrafanaLink from '../js/components/GrafanaLink.jsx';
import { routerWrap } from './testHelpers.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';
import Enzyme, { mount } from 'enzyme';

Enzyme.configure({ adapter: new Adapter() });
sinonStubPromise(sinon);

describe('GrafanaLink', () => {
  it('makes a link', () => {
    let api = ApiHelpers('');
    let linkProps = {
      resource: "Replication Controller",
      name: "aldksf-3409823049823",
      namespace: "myns",
      PrefixedLink: api.PrefixedLink
    };
    let component = mount(routerWrap(GrafanaLink, linkProps));

    let expectedDashboardNameStr = "/linkerd-replication-controller";
    let expectedNsStr = "var-namespace=myns";
    let expectedVarNameStr = "var-replication_controller=aldksf-3409823049823";

    expect(component.find("GrafanaLink")).to.have.length(1);
    expect(component.html()).to.contain(expectedDashboardNameStr);
    expect(component.html()).to.contain(expectedNsStr);
    expect(component.html()).to.contain(expectedVarNameStr);
  });
});

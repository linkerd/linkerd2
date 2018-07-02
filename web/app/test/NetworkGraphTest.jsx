// import _ from 'lodash';
import Adapter from 'enzyme-adapter-react-16';
import emojivotoPodFixtures from './fixtures/emojivotoPods.json';
import { expect } from 'chai';
// import nsFixtures from './fixtures/namespaces.json';
import { routerWrap } from './testHelpers.jsx';
import NetworkGraph from '../js/components/NetworkGraph.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';
import Enzyme, { mount } from 'enzyme';


Enzyme.configure({ adapter: new Adapter() });
sinonStubPromise(sinon);

describe('NetworkGraph', () => {
  let component, fetchStub;

  const defaultProps = {
    namespace: "test",
  };

  function withPromise(fn) {
    return component.find("NetworkGraph").instance().serverPromise.then(fn);
  }

  beforeEach(() => {
    fetchStub = sinon.stub(window, 'fetch').returnsPromise();
  });

  afterEach(() => {
    component = null;
    window.fetch.restore();
  });

  it("check if component renders", () => {
    let exampleData = {"ok": {"statTables":[]}};
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve(exampleData)
    });
    component = mount(routerWrap(NetworkGraph, defaultProps));

    return withPromise(() => {
      component.update();
      expect(component.find("NetworkGraph")).to.have.length(1);
      expect(component.find("ConduitSpinner")).to.have.length(0);
    });
  });

  it("check if conduitspinner renders when context is not loaded yet", () => {
    component = mount(routerWrap(NetworkGraph, defaultProps));
    expect(component.find("NetworkGraph")).to.have.length(1);
    expect(component.find("ConduitSpinner")).to.have.length(1);
  });

  it("check if graph renders when data is received", () => {
    // console.log("example data before fetch: ", emojivotoPodFixtures);
    fetchStub.resolves({
      ok: true,
      json: () => {
        console.log("calling it!!!");
        return Promise.resolve({metrics: ["hello"]});
      }
    });
    component = mount(routerWrap(NetworkGraph, defaultProps));

    return withPromise(() => {
      component.update();
      expect(component.find("NetworkGraph")).to.have.length(1);
      expect(component.html()).to.include("voting");
    });
  });

});

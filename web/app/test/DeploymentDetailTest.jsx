/* eslint-disable */
import 'raf/polyfill'; // the polyfill import must be first
import { ApiHelpers } from '../js/components/util/ApiHelpers.jsx';
import Adapter from 'enzyme-adapter-react-16';
import DeploymentDetail from '../js/components/DeploymentDetail.jsx';
import Enzyme from 'enzyme';
import { expect } from 'chai';
import { mount } from 'enzyme';
import { routerWrap } from "./testHelpers.jsx";
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';
/* eslint-enable */

Enzyme.configure({ adapter: new Adapter() });
sinonStubPromise(sinon);

describe('DeploymentDetail', () => {
  let component, fetchStub;

  function withPromise(fn) {
    return component.find("DeploymentDetail").instance().serverPromise.then(fn);
  }

  beforeEach(() => {
    fetchStub = sinon.stub(window, 'fetch');
  });

  afterEach(() => {
    component = null;
    window.fetch.restore();
  });

  it('renders the spinner before metrics are loaded', () => {
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ metrics: [] })
    });
    component = mount(routerWrap(DeploymentDetail));

    return withPromise(() => {
      expect(component.find("ConduitSpinner")).to.have.length(1);
    });
  });
});

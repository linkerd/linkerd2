import Deployment from '../js/components/Deployment.jsx';
import { expect } from 'chai';
import { mount } from 'enzyme';
import { routerWrap } from "./testHelpers.jsx";
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';

sinonStubPromise(sinon);

describe('Deployment', () => {
  let component, fetchStub;

  beforeEach(() => {
    fetchStub = sinon.stub(window, 'fetch');
  });

  afterEach(() => {
    window.fetch.restore();
  });

  it('renders the spinner before metrics are loaded', () => {
    fetchStub.returnsPromise().resolves({
      json: () => Promise.resolve({ metrics: [] })
    });
    component = mount(routerWrap(Deployment));
    expect(component.find("ConduitSpinner")).to.have.length(1);
  });
});

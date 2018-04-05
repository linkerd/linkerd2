/* eslint-disable */
import _ from 'lodash';
import 'raf/polyfill'; // the polyfill import must be first
import Adapter from 'enzyme-adapter-react-16';
import { ApiHelpers } from '../js/components/util/ApiHelpers.jsx';
import Enzyme from 'enzyme';
import { expect } from 'chai';
import { mount } from 'enzyme';
import { routerWrap } from './testHelpers.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';
/* eslint-enable */

Enzyme.configure({ adapter: new Adapter() });
sinonStubPromise(sinon);

describe('ApiHelpers', () => {
  let api, fetchStub;

  beforeEach(() => {
    fetchStub = sinon.stub(window, 'fetch');
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve({ metrics: [] })
    });
    api = ApiHelpers('');
  });

  afterEach(() => {
    api = null;
    window.fetch.restore();
  });

  describe('getMetricsWindow/setMetricsWindow', () => {
    it('sets a default metricsWindow', () => {
      expect(api.getMetricsWindow()).to.equal('10m');
    });

    it('changes the metricsWindow on valid window input', () => {
      expect(api.getMetricsWindow()).to.equal('10m');

      api.setMetricsWindow('10s');
      expect(api.getMetricsWindow()).to.equal('10s');

      api.setMetricsWindow('1m');
      expect(api.getMetricsWindow()).to.equal('1m');

      api.setMetricsWindow('10m');
      expect(api.getMetricsWindow()).to.equal('10m');
    });

    it('does not change metricsWindow on invalid window size', () => {
      expect(api.getMetricsWindow()).to.equal('10m');

      api.setMetricsWindow('10h');
      expect(api.getMetricsWindow()).to.equal('10m');
    });
  });

  describe('ConduitLink', () => {
    it('respects default values', () => {
      api = ApiHelpers('/my/path/prefix/web:/foo');
      let linkProps = { to: "/myrelpath", children: ["Informative Link Title"] };
      let conduitLink = mount(routerWrap(api.ConduitLink, linkProps));

      expect(conduitLink.find("Link")).to.have.length(1);
      expect(conduitLink.html()).to.contain('href="/my/path/prefix/web:/foo/myrelpath"');
      expect(conduitLink.html()).to.not.contain('target="_blank"');
      expect(conduitLink.html()).to.contain(linkProps.children[0]);
    });

    it('wraps a relative link with the pathPrefix', () => {
      api = ApiHelpers('/my/path/prefix');
      let linkProps = { to: "/myrelpath", children: ["Informative Link Title"] };
      let conduitLink = mount(routerWrap(api.ConduitLink, linkProps));

      expect(conduitLink.find("Link")).to.have.length(1);
      expect(conduitLink.html()).to.contain('href="/my/path/prefix/myrelpath"');
      expect(conduitLink.html()).to.contain(linkProps.children[0]);
    });

    it('wraps a relative link with no pathPrefix', () => {
      api = ApiHelpers('');
      let linkProps = { to: "/myrelpath", children: ["Informative Link Title"] };
      let conduitLink = mount(routerWrap(api.ConduitLink, linkProps));

      expect(conduitLink.find("Link")).to.have.length(1);
      expect(conduitLink.html()).to.contain('href="/myrelpath"');
      expect(conduitLink.html()).to.contain(linkProps.children[0]);
    });

    it('replaces the deployment in a pathPrefix', () => {
      api = ApiHelpers('/my/path/prefix/web:/foo');
      let linkProps = { deployment: "mydeployment", to: "/myrelpath", children: ["Informative Link Title"] };
      let conduitLink = mount(routerWrap(api.ConduitLink, linkProps));

      expect(conduitLink.find("Link")).to.have.length(1);
      expect(conduitLink.html()).to.contain('href="/my/path/prefix/mydeployment:/foo/myrelpath"');
      expect(conduitLink.html()).to.contain(linkProps.children[0]);
    });

    it('sets target=blank', () => {
      api = ApiHelpers('/my/path/prefix');
      let linkProps = { targetBlank: true, to: "/myrelpath", children: ["Informative Link Title"] };
      let conduitLink = mount(routerWrap(api.ConduitLink, linkProps));

      expect(conduitLink.find("Link")).to.have.length(1);
      expect(conduitLink.html()).to.contain('target="_blank"');
      expect(conduitLink.html()).to.contain(linkProps.children[0]);
    });
  });

  describe('makeCancelable', () => {
    it('wraps the original promise', () => {
      let p = Promise.resolve({ result: 'my response', ok: true });
      let cancelablePromise = api.makeCancelable(p);

      return cancelablePromise.promise
        .then(resp => {
          expect(resp.result).to.equal('my response');
        });
    });

    it('returns an error on original promise rejection', () => {
      let p = Promise.reject({ rejectionReason: 'it is bad' });
      let cancelablePromise = api.makeCancelable(p);

      return cancelablePromise.promise
        .then(() => {
          return Promise.reject('Expected method to reject.');
        })
        .catch(e => {
          expect(e).to.deep.equal({ rejectionReason: 'it is bad' });
        });
    });

    it('returns an error if the fetch did not go well', () => {
      let reason = { rejectionReason: 'it is bad' };
      let p = Promise.reject(reason);
      let cancelablePromise = api.makeCancelable(p);

      return cancelablePromise.promise
        .then(() => {
          return Promise.reject('Expected method to reject.');
        })
        .catch(e => {
          expect(e).to.deep.equal(reason);
        });
    });

    it('calls the provided success handler on response success', () => {
      let onSuccess = sinon.spy();
      let fakeFetchResults = { result: 5, ok: true };
      let p = Promise.resolve(fakeFetchResults);
      let cancelablePromise = api.makeCancelable(p, onSuccess);

      return cancelablePromise.promise
        .then(() => {
          expect(onSuccess.calledOnce).to.be.true;
          expect(onSuccess.args[0][0]).to.deep.equal(fakeFetchResults);
        });
    });

    it('allows you to cancel a promise', () => {
      let p = Promise.resolve({ result: 'my response', ok: true });
      let cancelablePromise = api.makeCancelable(p);
      cancelablePromise.cancel();

      return cancelablePromise.promise
        .then(() => {
          return Promise.reject('Expected method to reject.');
        }).catch(resp => {
          expect(resp.isCanceled).to.be.true;
        });
    });
  });

  describe('fetch', () => {
    it('adds pathPrefix to a metrics request', () => {
      api = ApiHelpers('/the/path/prefix');
      api.fetch('/resource/foo');

      expect(fetchStub.calledOnce).to.be.true;
      expect(fetchStub.args[0][0]).to.equal('/the/path/prefix/resource/foo');
    });

    it('requests from / when there is no path prefix', () => {
      api = ApiHelpers('');
      api.fetch('/resource/foo');

      expect(fetchStub.calledOnce).to.be.true;
      expect(fetchStub.args[0][0]).to.equal('/resource/foo');
    });

    it('throws an error if response status is not "ok"', () => {
      let errorMessage = "do or do not. there is no try.";
      fetchStub.returnsPromise().resolves({
        ok: false,
        statusText: errorMessage
      });

      api = ApiHelpers('');
      let errorHandler = sinon.spy();

      let f = api.fetch('/resource/foo');

      return f.promise
        .then(() => {
          return Promise.reject('Expected method to reject.');
        }, errorHandler)
        .then(() => {
          expect(errorHandler.args[0][0].message).to.equal(errorMessage);
          expect(errorHandler.calledOnce).to.be.true;
        });
    });

    it('correctly passes through rejection messages', () => {
      let rejectionMessage = "hm, an error";
      fetchStub.returnsPromise().rejects({
        myReason: rejectionMessage
      });

      api = ApiHelpers('');
      let rejectHandler = sinon.spy();

      let f = api.fetch('/resource/foo');

      return f.promise
        .then(() => {
          return Promise.reject('Expected method to reject.');
        }, rejectHandler)
        .then(() => {
          expect(rejectHandler.args[0][0]).to.have.own.property('myReason');
          expect(rejectHandler.args[0][0].myReason).to.equal(rejectionMessage);
          expect(rejectHandler.calledOnce).to.be.true;
        });
    });
  });

  describe('fetchMetrics', () => {
    it('adds pathPrefix and metricsWindow to a metrics request', () => {
      api = ApiHelpers('/the/prefix');
      api.fetchMetrics('/my/path');

      expect(fetchStub.calledOnce).to.be.true;
      expect(fetchStub.args[0][0]).to.equal('/the/prefix/my/path?window=10m');
    });

    it('adds a ?window= if metricsWindow is the only param', () => {
      api.fetchMetrics('/metrics');

      expect(fetchStub.calledOnce).to.be.true;
      expect(fetchStub.args[0][0]).to.equal('/metrics?window=10m');
    });

    it('adds &window= if metricsWindow is not the only param', () => {
      api.fetchMetrics('/metrics?foo=3&bar="me"');

      expect(fetchStub.calledOnce).to.be.true;
      expect(fetchStub.args[0][0]).to.equal('/metrics?foo=3&bar="me"&window=10m');
    });

    it('does not add another &window= if there is already a window param', () => {
      api.fetchMetrics('/metrics?foo=3&window=24h&bar="me"');

      expect(fetchStub.calledOnce).to.be.true;
      expect(fetchStub.args[0][0]).to.equal('/metrics?foo=3&window=24h&bar="me"');
    });
  });

  describe('fetchPods', () => {
    it('fetches the pods from the api', () => {
      api = ApiHelpers("/random/prefix");
      api.fetchPods();

      expect(fetchStub.calledOnce).to.be.true;
      expect(fetchStub.args[0][0]).to.equal('/random/prefix/api/pods');
    });
  });

  describe('urlsForResource', () => {
    it('returns the correct timeseries and metric rollup urls for deployment overviews', () => {
      api = ApiHelpers('/go/my/own/way');
      let deploymentUrls = api.urlsForResource["deployment"].url("myDeploy");

      expect(deploymentUrls.ts).to.equal('/api/metrics?&timeseries=true&target_deploy=myDeploy');
      expect(deploymentUrls.rollup).to.equal('/api/metrics?&target_deploy=myDeploy');
    });

    it('returns the correct timeseries and metric rollup urls for upstream deployments', () => {
      let deploymentUrls = api.urlsForResource["upstream_deployment"].url("farUp");

      expect(deploymentUrls.ts).to.equal('/api/metrics?&aggregation=source_deploy&target_deploy=farUp&timeseries=true');
      expect(deploymentUrls.rollup).to.equal('/api/metrics?&aggregation=source_deploy&target_deploy=farUp');
    });
  });
});

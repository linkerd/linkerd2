/* eslint-disable */
import 'raf/polyfill'; // the polyfill import must be first
import ApiHelpers from './ApiHelpers.jsx';
import { mount } from 'enzyme';
import { routerWrap } from '../../../test/testHelpers.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';
/* eslint-enable */

sinonStubPromise(sinon);

describe('ApiHelpers', () => {
  let api, fetchStub;

  beforeEach(() => {
    fetchStub = sinon.stub(window, 'fetch');
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({}),
      text: () => Promise.resolve({})
    });
    api = ApiHelpers('');
  });

  afterEach(() => {
    api = null;
    window.fetch.restore();
  });

  describe('getMetricsWindow/setMetricsWindow', () => {
    it('sets a default metricsWindow', () => {
      expect(api.getMetricsWindow()).toEqual('1m');
    });

    it('changes the metricsWindow on valid window input', () => {
      expect(api.getMetricsWindow()).toEqual('1m');

      api.setMetricsWindow('10s');
      expect(api.getMetricsWindow()).toEqual('10s');

      api.setMetricsWindow('1m');
      expect(api.getMetricsWindow()).toEqual('1m');

      api.setMetricsWindow('10m');
      expect(api.getMetricsWindow()).toEqual('10m');

      api.setMetricsWindow('1h');
      expect(api.getMetricsWindow()).toEqual('1h');
    });

    it('does not change metricsWindow on invalid window size', () => {
      expect(api.getMetricsWindow()).toEqual('1m');

      api.setMetricsWindow('10h');
      expect(api.getMetricsWindow()).toEqual('1m');
    });
  });

  describe('PrefixedLink', () => {
    it('respects default values', () => {
      api = ApiHelpers('/my/path/prefix/linkerd-web:/foo');
      let linkProps = { to: "/myrelpath", children: ["Informative Link Title"] };
      let prefixedLink = mount(routerWrap(api.PrefixedLink, linkProps));

      expect(prefixedLink.find("Link")).toHaveLength(1);
      expect(prefixedLink.find('a')).toHaveProp('href', '/my/path/prefix/linkerd-web:/foo/myrelpath');
      expect(prefixedLink.find('a')).not.toHaveProp('target', '_blank');
      expect(prefixedLink).toIncludeText(linkProps.children[0]);
    });

    it('wraps a relative link with the pathPrefix', () => {
      api = ApiHelpers('/my/path/prefix');
      let linkProps = { to: "/myrelpath", children: ["Informative Link Title"] };
      let prefixedLink = mount(routerWrap(api.PrefixedLink, linkProps));

      expect(prefixedLink.find("Link")).toHaveLength(1);
      expect(prefixedLink.find('a')).toHaveProp('href', '/my/path/prefix/myrelpath');
      expect(prefixedLink).toIncludeText(linkProps.children[0]);
    });

    it('wraps a relative link with no pathPrefix', () => {
      api = ApiHelpers('');
      let linkProps = { to: "/myrelpath", children: ["Informative Link Title"] };
      let prefixedLink = mount(routerWrap(api.PrefixedLink, linkProps));

      expect(prefixedLink.find("Link")).toHaveLength(1);
      expect(prefixedLink.find('a')).toHaveProp('href', '/myrelpath');
      expect(prefixedLink).toIncludeText(linkProps.children[0]);
    });

    it('sets target=blank', () => {
      api = ApiHelpers('/my/path/prefix');
      let linkProps = { targetBlank: true, to: "/myrelpath", children: ["Informative Link Title"] };
      let prefixedLink = mount(routerWrap(api.PrefixedLink, linkProps));

      expect(prefixedLink.find("Link")).toHaveLength(1);
      expect(prefixedLink.find('a')).toHaveProp('target', '_blank');
      expect(prefixedLink).toIncludeText(linkProps.children[0]);
    });
  });

  describe('makeCancelable', () => {
    it('wraps the original promise', () => {
      let p = Promise.resolve({ result: 'my response', ok: true });
      let cancelablePromise = api.makeCancelable(p);

      return cancelablePromise.promise
        .then(resp => {
          expect(resp.result).toEqual('my response');
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
          expect(e).toEqual({ rejectionReason: 'it is bad' });
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
          expect(e).toEqual(reason);
        });
    });

    it('calls the provided success handler on response success', () => {
      let onSuccess = sinon.spy();
      let fakeFetchResults = { result: 5, ok: true };
      let p = Promise.resolve(fakeFetchResults);
      let cancelablePromise = api.makeCancelable(p, onSuccess);

      return cancelablePromise.promise
        .then(() => {
          expect(onSuccess.calledOnce).toBeTruthy;
          expect(onSuccess.args[0][0]).toEqual(fakeFetchResults);
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
          expect(resp.isCanceled).toBeTruthy;
        });
    });
  });

  describe('fetch', () => {
    it('adds pathPrefix to a metrics request', () => {
      api = ApiHelpers('/the/path/prefix');
      api.fetch('/resource/foo');

      expect(fetchStub.calledOnce).toBeTruthy;
      expect(fetchStub.args[0][0]).toEqual('/the/path/prefix/resource/foo');
    });

    it('requests from / when there is no path prefix', () => {
      api = ApiHelpers('');
      api.fetch('/resource/foo');

      expect(fetchStub.calledOnce).toBeTruthy;
      expect(fetchStub.args[0][0]).toEqual('/resource/foo');
    });

    it('throws an error if response status is not "ok"', () => {
      let errorMessage = "do or do not. there is no try.";
      fetchStub.resolves({
        ok: false,
        json: () => Promise.resolve({
          error: errorMessage
        }),
      });

      api = ApiHelpers('');
      let errorHandler = sinon.spy();

      let f = api.fetch('/resource/foo');

      return f.promise
        .then(() => {
          return Promise.reject('Expected method to reject.');
        }, errorHandler)
        .then(() => {
          expect(errorHandler.args[0][0].error).toEqual(errorMessage);
          expect(errorHandler.calledOnce).toBeTruthy;
        });
    });

    it('correctly passes through rejection messages', () => {
      let rejectionMessage = "hm, an error";
      fetchStub.rejects({
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
          expect(rejectHandler.args[0][0]).toHaveProperty('myReason');
          expect(rejectHandler.args[0][0].myReason).toEqual(rejectionMessage);
          expect(rejectHandler.calledOnce).toBeTruthy;
        });
    });
  });

  describe('fetchMetrics', () => {
    it('adds pathPrefix and metricsWindow to a metrics request', () => {
      api = ApiHelpers('/the/prefix');
      api.fetchMetrics('/my/path');

      expect(fetchStub.calledOnce).toBeTruthy;
      expect(fetchStub.args[0][0]).toEqual('/the/prefix/my/path?window=1m');
    });

    it('adds a ?window= if metricsWindow is the only param', () => {
      api.fetchMetrics('/api/tps-reports');

      expect(fetchStub.calledOnce).toBeTruthy;
      expect(fetchStub.args[0][0]).toEqual('/api/tps-reports?window=1m');
    });

    it('adds &window= if metricsWindow is not the only param', () => {
      api.fetchMetrics('/api/tps-reports?foo=3&bar="me"');

      expect(fetchStub.calledOnce).toBeTruthy;
      expect(fetchStub.args[0][0]).toEqual('/api/tps-reports?foo=3&bar="me"&window=1m');
    });

    it('does not add another &window= if there is already a window param', () => {
      api.fetchMetrics('/api/tps-reports?foo=3&window=24h&bar="me"');

      expect(fetchStub.calledOnce).toBeTruthy;
      expect(fetchStub.args[0][0]).toEqual('/api/tps-reports?foo=3&window=24h&bar="me"');
    });
  });

  describe('fetchPods', () => {
    it('fetches the pods from the api', () => {
      api = ApiHelpers("/random/prefix");
      api.fetchPods();

      expect(fetchStub.calledOnce).toBeTruthy;
      expect(fetchStub.args[0][0]).toEqual('/random/prefix/api/pods');
    });
  });

  describe('urlsForResource', () => {
    it('returns the correct rollup url for deployment overviews', () => {
      api = ApiHelpers('/go/my/own/way');
      let deploymentUrl = api.urlsForResource("deployment");
      expect(deploymentUrl).toEqual('/api/tps-reports?resource_type=deployment&all_namespaces=true');
    });

    it('returns the correct rollup url for pod overviews', () => {
      api = ApiHelpers('/go/my/own/way');
      let deploymentUrls = api.urlsForResource("pod");
      expect(deploymentUrls).toEqual('/api/tps-reports?resource_type=pod&all_namespaces=true');
    });

    it('scopes the query to the provided namespace', () => {
      api = ApiHelpers('/go/my/own/way');
      let deploymentUrls = api.urlsForResource("pod", "my-ns");
      expect(deploymentUrls).toEqual('/api/tps-reports?resource_type=pod&namespace=my-ns');
    });

    it('queries for TCP stats when specified', () => {
      api = ApiHelpers();
      let url = api.urlsForResource('sts', '', true);
      expect(url).toEqual('/api/tps-reports?resource_type=sts&all_namespaces=true&tcp_stats=true');
    })
  });

  describe('fetchCheck', () => {
    it('fetches checks from the api', () => {
      api = ApiHelpers();
      api.fetchCheck();

      expect(fetchStub.calledOnce).toBeTruthy();
      expect(fetchStub.args[0][0]).toEqual('/api/check');
    });
  });

  describe('fetchResourceDefinition', () => {
    it('fetches the resource definition from the api', () => {
      const [namespace, type, name] = ["namespace", "type", "name"];
      api = ApiHelpers();
      api.fetchResourceDefinition(namespace, type, name);

      expect(fetchStub.calledOnce).toBeTruthy;
      expect(fetchStub.args[0][0]).toEqual(`/api/resource-definition?namespace=${namespace}&resource_type=${type}&resource_name=${name}`);
    });
  });

  describe('fetchGateways', () => {
    it('fetches the gateways from the api', () => {
      api = ApiHelpers();
      api.fetchGateways();

      expect(fetchStub.calledOnce).toBeTruthy;
      expect(fetchStub.args[0][0]).toEqual('/api/gateways');
    });
  });
});

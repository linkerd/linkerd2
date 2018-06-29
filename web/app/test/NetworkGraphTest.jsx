// import _ from 'lodash';
import Adapter from 'enzyme-adapter-react-16';
// import conduitPodFixtures from './fixtures/conduitPods.json';
import { expect } from 'chai';
// import nsFixtures from './fixtures/namespaces.json';
import { routerWrap } from './testHelpers.jsx';
import NetworkGraph from '../js/components/NetworkGraph.jsx';
import sinon from 'sinon';
import sinonStubPromise from 'sinon-stub-promise';
import Enzyme, { mount } from 'enzyme';


Enzyme.configure({ adapter: new Adapter() });
sinonStubPromise(sinon);

let exampleData = {"ok":
{"statTables":[
  {"podGroup":
        {"rows":[
          {"resource":{"namespace":"emojivoto","type":"deployments","name":"voting"},
            "timeWindow":"1m","meshedPodCount":"1","runningPodCount":"1","failedPodCount":"0",
            "stats":
                {"successCount":"52","failureCount":"65","latencyMsP50":"5","latencyMsP95":"10",
                  "latencyMsP99":"10","tlsRequestCount":"0"}},{"resource":{"namespace":"emojivoto",
            "type":"deployments","name":"web"},"timeWindow":"1m","meshedPodCount":"1",
          "runningPodCount":"1","failedPodCount":"0","stats":{"successCount":"169",
            "failureCount":"64","latencyMsP50":"5","latencyMsP95":"10","latencyMsP99":"18",
            "tlsRequestCount":"0"}},{"resource":{"namespace":"emojivoto","type":"deployments",
            "name":"vote-bot"},"timeWindow":"1m","meshedPodCount":"1","runningPodCount":"1",
          "failedPodCount":"0","stats":null},{"resource":{"namespace":"emojivoto",
            "type":"deployments","name":"emoji"},"timeWindow":"1m","meshedPodCount":"1",
          "runningPodCount":"1","failedPodCount":"0","stats":{"successCount":"286",
            "failureCount":"64","latencyMsP50":"5","latencyMsP95":"9","latencyMsP99":"10",
            "tlsRequestCount":"0"}}]}},
  {"podGroup":{"rows":[{"resource":{"namespace":"emojivoto","type":"services","name":"emoji-svc"},
    "timeWindow":"1m","meshedPodCount":"1","runningPodCount":"1","failedPodCount":"0","stats":null},
  {"resource":{"namespace":"emojivoto","type":"services","name":"voting-svc"},"timeWindow":"1m",
    "meshedPodCount":"1","runningPodCount":"1","failedPodCount":"0","stats":null},
  {"resource":{"namespace":"emojivoto","type":"services","name":"web-svc"},"timeWindow":"1m",
    "meshedPodCount":"1","runningPodCount":"1","failedPodCount":"0","stats":null}]}},
  {"podGroup":{"rows":[]}},{"podGroup":{"rows":[{"resource":{"namespace":"emojivoto","type":"pods",
    "name":"voting-5cb8f85965-2rkjr"},"timeWindow":"1m","meshedPodCount":"1","runningPodCount":"1",
  "failedPodCount":"0","stats":{"successCount":"52","failureCount":"65","latencyMsP50":"5",
    "latencyMsP95":"10","latencyMsP99":"10","tlsRequestCount":"0"}},
  {"resource":{"namespace":"emojivoto","type":"pods","name":"web-5ddd56d679-hsgn7"},
    "timeWindow":"1m","meshedPodCount":"1","runningPodCount":"1","failedPodCount":"0",
    "stats":{"successCount":"169","failureCount":"64","latencyMsP50":"5","latencyMsP95":"10",
      "latencyMsP99":"18","tlsRequestCount":"0"}},{"resource":{"namespace":"emojivoto","type":"pods",
    "name":"vote-bot-86f685b974-cth8h"},"timeWindow":"1m","meshedPodCount":"1","runningPodCount":"1",
  "failedPodCount":"0","stats":null},{"resource":{"namespace":"emojivoto","type":"pods","name":"emoji-57d4797b68-29fw7"},
    "timeWindow":"1m","meshedPodCount":"1","runningPodCount":"1","failedPodCount":"0",
    "stats":{"successCount":"286","failureCount":"64","latencyMsP50":"5","latencyMsP95":"9",
      "latencyMsP99":"10","tlsRequestCount":"0"}}]}}]}};

describe('NetworkGraph', () => {
  let component, fetchStub;

  const defaultProps = {
    namespace: "test",
  };

  // let mockNodes = [{"id": "voting", "r": 15}, {"id": "emoji", "r": 15}];
  // let mockLinks = [{"source": {"id": "voting", "r": 15}, "target": {"id": "emoji", "r": 15}, "successRate": 0.95}];



  function withPromise(fn) {
    console.log("promise: ", component.find("NetworkGraph").instance().serverPromise.then(fn));
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
    fetchStub.resolves({
      ok: true,
      json: () => Promise.resolve({})
    });
    component = mount(routerWrap(NetworkGraph, defaultProps));

    return withPromise(() => {
      // component.update();
      expect(component.find("NetworkGraph")).to.have.length(1);
      // expect(component.find("ConduitSpinner")).to.have.length(0);
      // expect(component.html()).to.include("voting");
      // done();
    });
  });
  it("check if graph renders", () => {
    console.log("example datat before fetch: ", exampleData);
    // etchStub.returnsPromise().resolves(
    fetchStub.returnsPromise().resolves({
      ok: true,
      json: () => Promise.resolve(exampleData)
    });
    component = mount(routerWrap(NetworkGraph, defaultProps));

    return withPromise(() => {
      // component.update();
      console.log("COMPONENT: ", component.html());
      expect(component.find("NetworkGraph")).to.have.length(1);
      // expect(mockNodes).to.have.length(2);
      // expect(mockLinks).to.have.length(1);
      // expect(component.find("ConduitSpinner")).to.have.length(0);
      expect(component.html()).to.include("voting");
      // done();
    });
  });
  // it("check if component renders", () => {
  //   fetchStub.resolves({
  //     ok: true,
  //     json: () => Promise.resolve({ nodes: mockNodes, links: mockLinks })
  //   });
  //   component = mount(routerWrap(NetworkGraph, defaultProps));

  //   return withPromise(() => {
  //     // component.update();
  //     expect(component.find("NetworkGraph")).to.have.length(1);
  //     // expect(component.find("ConduitSpinner")).to.have.length(0);
  //     // expect(component.html()).to.include("voting");
  //     // done();
  //   });
  // });

});

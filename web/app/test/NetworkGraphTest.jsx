import Adapter from 'enzyme-adapter-react-16';
import emojivotoPodFixtures from './fixtures/emojivotoPods.json';
import { expect } from 'chai';
import { NetworkGraphBase } from '../js/components/NetworkGraph.jsx';
import React from 'react';
import Enzyme, { shallow } from 'enzyme';


Enzyme.configure({ adapter: new Adapter() });

const deploys = [
  {name: "emoji", namespace: "emojivoto", totalRequests: 120, requestRate: 2, successRate: 1},
  {name: "vote-bot", namespace: "emojivoto", totalRequests: 0, requestRate: null, successRate: null},
  {name: "voting", namespace: "emojivoto", totalRequests: 59, requestRate: 0.9833333333333333, successRate: 0.7288135593220338},
  {name: "web", namespace: "emojivoto", totalRequests: 117, requestRate: 1.95, successRate: 0.8803418803418803}
];

describe("NetworkGraph", () => {

  it("checks graph data", () => {
    const component = shallow(
      <NetworkGraphBase
        data={emojivotoPodFixtures}
        deployments={deploys} />
    );

    const data = component.instance().getGraphData();
    expect(data.links).to.have.length(3);
    expect(data.nodes).to.have.length(4);
    expect(data.links[0]).to.include({source: "web", target: "emoji"});
    expect(data.nodes[0]).to.include({ id: "web", r: 15 });
  });
});

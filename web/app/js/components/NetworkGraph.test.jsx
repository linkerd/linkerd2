import emojivotoPodFixtures from '../../test/fixtures/emojivotoPods.json';
import { NetworkGraphBase } from './NetworkGraph.jsx';
import React from 'react';
import { shallow } from 'enzyme';

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
    expect(data.links).toHaveLength(3);
    expect(data.nodes).toHaveLength(4);
    expect(data.links[0]).toEqual({source: "web", target: "emoji"});
    expect(data.nodes[0]).toEqual({ id: "web"});
  });
});

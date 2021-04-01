import 'whatwg-fetch';
import { forceCenter, forceLink, forceManyBody, forceSimulation } from 'd3-force';
import { select, selectAll } from 'd3-selection';
import PropTypes from 'prop-types';
import React from 'react';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _map from 'lodash/map';
import _uniq from 'lodash/uniq';
import { drag } from 'd3-drag';
import { format } from 'd3-format';
import { metricsPropType } from './util/MetricUtils.jsx';
import { withContext } from './util/AppContext.jsx';
import withREST from './util/withREST.jsx';

// create a Object with only the subset of functions/submodules/plugins that we need
const d3 = {
  drag,
  forceCenter,
  forceLink,
  forceManyBody,
  forceSimulation,
  select,
  selectAll,
  format,
};

const defaultSvgWidth = 524;
const defaultSvgHeight = 325;
const defaultNodeRadius = 15;
const margin = { top: 0, right: 0, bottom: 10, left: 0 };

const simulation = d3.forceSimulation()
  .force('link',
    d3.forceLink()
      .id(d => d.id)
      .distance(140))
  .force('charge', d3.forceManyBody().strength(-20))
  .force('center', d3.forceCenter(defaultSvgWidth / 2, defaultSvgHeight / 2));

export class NetworkGraphBase extends React.Component {
  constructor(props) {
    super(props);

    // https://github.com/d3/d3-zoom/issues/32
    d3.getEvent = (() => require('d3-selection').event); // eslint-disable-line global-require
  }

  componentDidMount() {
    const container = document.getElementsByClassName('network-graph-container')[0];
    const width = !container ? defaultSvgWidth : container.getBoundingClientRect().width;

    this.svg = d3.select('.network-graph-container')
      .append('svg')
      .attr('class', 'network-graph')
      .attr('width', width)
      .attr('height', width)
      .append('g')
      .attr('transform', `translate(${margin.left},${margin.top})`);
  }

  componentDidUpdate() {
    simulation.alpha(1).restart();
    this.drawGraph();
  }

  getGraphData() {
    const { data, deployments } = this.props;
    const links = [];
    const nodeList = [];

    _map(data, (resp, i) => {
      const rows = _get(resp, ['ok', 'statTables', 0, 'podGroup', 'rows']);
      const dst = deployments[i].name;
      _map(rows, row => {
        links.push({
          source: row.resource.name,
          target: dst,
        });
        nodeList.push(row.resource.name);
        nodeList.push(dst);
      });
    });

    const nodes = _map(_uniq(nodeList), n => ({ id: n }));
    return {
      links,
      nodes,
    };
  }

  drawGraph() {
    const graphData = this.getGraphData();

    // check if graph is present to prevent drawing of multiple graphs
    if (this.svg.select('circle')._groups[0][0]) {
      return;
    }
    this.drawGraphComponents(graphData.links, graphData.nodes);
  }

  drawGraphComponents(links, nodes) {
    if (_isEmpty(nodes)) {
      d3.select('.network-graph-container').select('svg').attr('height', 0);
      return;
    } else {
      d3.select('.network-graph-container').select('svg').attr('height', defaultSvgHeight);
    }

    this.svg.append('svg:defs').selectAll('marker')
      .data(links) // Different link/path types can be defined here
      .enter()
      .append('svg:marker') // This section adds in the arrows
      .attr('id', node => `${node.source}/${node.target}`)
      .attr('viewBox', '0 -5 10 10')
      .attr('refX', 24)
      .attr('refY', -0.25)
      .attr('markerWidth', 3)
      .attr('markerHeight', 3)
      .attr('fill', '#454242')
      .attr('orient', 'auto')
      .append('svg:path')
      .attr('d', 'M0,-5L10,0L0,5');

    // add the links and the arrows
    const path = this.svg.append('svg:g').selectAll('path')
      .data(links)
      .enter()
      .append('svg:path')
      .attr('stroke-width', 3)
      .attr('stroke', '#454242')
      .attr('marker-end', node => `url(#${node.source}/${node.target})`);

    const nodeElements = this.svg.append('g')
      .selectAll('circle')
      .data(nodes)
      .enter()
      .append('circle')
      .attr('r', defaultNodeRadius)
      .attr('fill', 'steelblue')
      .call(d3.drag()
        .on('start', this.dragstarted)
        .on('drag', this.dragged)
        .on('end', this.dragended));

    const textElements = this.svg.append('g')
      .selectAll('text')
      .data(nodes)
      .enter()
      .append('text')
      .text(node => node.id)
      .attr('font-size', 15)
      .attr('dx', 20)
      .attr('dy', 4);

    simulation.nodes(nodes).on('tick', () => {
      path
        .attr('d', node => `M${node.source.x} ${node.source.y} L ${node.target.x} ${node.target.y}`);

      nodeElements
        .attr('cx', node => node.x)
        .attr('cy', node => node.y);

      textElements
        .attr('x', node => node.x)
        .attr('y', node => node.y);
    });

    simulation.force('link')
      .links(links);
  }

  dragstarted = (event, d) => {
    if (!event.active) {
      simulation.alphaTarget(0.3).restart();
    }
    d.x = event.x;
    d.y = event.y;
  }

  dragged = (event, d) => {
    d.x = event.x;
    d.y = event.y;
  }

  dragended = (event, d) => {
    if (!event.active) {
      simulation.alphaTarget(0);
    }
    d.x = event.x;
    d.y = event.y;
  }

  render() {
    return (
      <div>
        <div className="network-graph-container" />
      </div>
    );
  }
}

NetworkGraphBase.defaultProps = {
  deployments: [],
};

NetworkGraphBase.propTypes = {
  data: PropTypes.arrayOf(metricsPropType.isRequired).isRequired,
  deployments: PropTypes.arrayOf(PropTypes.object),
};

export default withREST(
  withContext(NetworkGraphBase),
  ({ api, namespace, deployments }) => {
    return _map(deployments, d => {
      return api.fetchMetrics(`${api.urlsForResource('deployment', namespace)}&to_name=${d.name}`);
    });
  },
  {
    poll: false,
    resetProps: ['deployment'],
  },
);

import _ from 'lodash';
import { metricsPropType } from './util/MetricUtils.js';
import PropTypes from 'prop-types';
import React from 'react';
import { withContext } from './util/AppContext.jsx';
import withREST from './util/withREST.jsx';
import * as d3 from 'd3';
import 'whatwg-fetch';


const defaultSvgWidth = 524;
const defaultSvgHeight = 325;
const margin = { top: 0, right: 0, bottom: 10, left: 0 };

const simulation = d3.forceSimulation()
  .force("link", d3.forceLink().id(function(d) { return d.id; }).distance(140))
  .force('charge', d3.forceManyBody().strength(-20))
  .force('center', d3.forceCenter(defaultSvgWidth / 2, defaultSvgHeight / 2));

export class NetworkGraphBase extends React.Component {
  static defaultProps = {
    deployments: []
  }

  static propTypes = {
    data: PropTypes.arrayOf(metricsPropType.isRequired).isRequired,
    deployments: PropTypes.arrayOf(PropTypes.object),
  }

  constructor(props) {
    super(props);
  }

  componentDidMount() {
    this.svg = d3.select(".network-graph-container")
      .append("svg")
      .attr("class", "network-graph")
      .attr("width", defaultSvgWidth)
      .attr("height", defaultSvgHeight)
      .append("g")
      .attr("transform", "translate(" + margin.left + "," + margin.top + ")");
  }

  componentDidUpdate() {
    simulation.alpha(1).restart();
    this.drawGraph();
  }

  getGraphData() {
    const { data } = this.props;
    let links = [];
    let nodeList = [];

    _.map(data, (r, i) => {
      let rows = _.get(r, ["ok", "statTables", 0, "podGroup", "rows"]);
      let dst = this.props.deployments[i].name;
      _.map(rows, row => {
        links.push({
          source: row.resource.name,
          target: dst,
        });
        nodeList.push(row.resource.name);
        nodeList.push(dst);
      });
    });

    let nodes = _.map(_.uniq(nodeList), n => ({ id: n, r: 15 }));
    return {"links": links, "nodes": nodes};
  }

  drawGraph() {
    let graphData = this.getGraphData();

    // check if graph is present to prevent drawing of multiple graphs
    if (this.svg.select("circle")._groups[0][0]) {
      return;
    }
    this.drawGraphComponents(graphData.links, graphData.nodes);
  }

  drawGraphComponents(links, nodes) {
    this.svg.append("svg:defs").selectAll("marker")
      .data(links)      // Different link/path types can be defined here
      .enter().append("svg:marker")    // This section adds in the arrows
      .attr("id", node => node.source + "/" + node.target)
      .attr("viewBox", "0 -5 10 10")
      .attr("refX", 24)
      .attr("refY", -0.25)
      .attr("markerWidth", 3)
      .attr("markerHeight", 3)
      .attr("fill", "#454242")
      .attr("orient", "auto")
      .append("svg:path")
      .attr("d", "M0,-5L10,0L0,5");

    // add the links and the arrows
    const path = this.svg.append("svg:g").selectAll("path")
      .data(links)
      .enter().append("svg:path")
      .attr("stroke-width", 3)
      .attr("stroke", "#454242")
      .attr("marker-end", node => "url(#"+node.source + "/" + node.target+")");

    const nodeElements = this.svg.append('g')
      .selectAll('circle')
      .data(nodes)
      .enter().append('circle')
      .attr("r", 15)
      .attr('fill', 'steelblue')
      .call(d3.drag()
        .on("start", this.dragstarted)
        .on("drag", this.dragged)
        .on("end", this.dragended));

    const textElements = this.svg.append('g')
      .selectAll('text')
      .data(nodes)
      .enter().append('text')
      .text(node => node.id)
      .attr('font-size', 15)
      .attr('dx', 20)
      .attr('dy', 4);

    simulation.nodes(nodes).on("tick", () => {
      path
        .attr("d", node =>  "M" +
              node.source.x + " " +
              node.source.y + " L " +
              node.target.x + " " +
              node.target.y);

      nodeElements
        .attr("cx", node => node.x)
        .attr("cy", node => node.y);

      textElements
        .attr("x", node => node.x)
        .attr("y", node => node.y);
    });

    simulation.force("link")
      .links(links);
  }

  dragstarted = d => {
    if (!d3.event.active) {simulation.alphaTarget(0.3).restart();}
    d.fx = d.x;
    d.fy = d.y;
  }

  dragged = d => {
    d.fx = d3.event.x;
    d.fy = d3.event.y;
  }

  dragended = d => {
    if (!d3.event.active) {simulation.alphaTarget(0);}
    d.fx = null;
    d.fy = null;
  }

  render() {
    return (
      <div>
        <div className="network-graph-container" />
      </div>
    );
  }
}

export default withREST(
  withContext(NetworkGraphBase),
  ({api, namespace, deployments}) =>
    _.map(deployments, d => {
      return api.fetchMetrics(api.urlsForResource("deployment", namespace) + "&to_name=" + d.name);
    }),
  {
    poll: false,
    resetProps: ["deployment"],
  },
);

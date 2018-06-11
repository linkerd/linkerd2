import _ from 'lodash';
import ErrorBanner from './ErrorBanner.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { withContext } from './util/AppContext.jsx';
import * as d3 from 'd3';
import 'whatwg-fetch';


const defaultSvgWidth = 494;
const defaultSvgHeight = 295;
const margin = { top: 0, right: 0, bottom: 10, left: 0 };

const simulation = d3.forceSimulation()
  .force("link", d3.forceLink().id(function(d) { return d.id; }).distance(60))
  .force('charge', d3.forceManyBody().strength(-20))
  .force('center', d3.forceCenter(defaultSvgWidth / 2, defaultSvgHeight / 2));

class NetworkGraph extends React.Component {
  static defaultProps = {
    deployments: []
  }

  static propTypes = {
    api: PropTypes.shape({
      cancelCurrentRequests: PropTypes.func.isRequired,
      fetchMetrics: PropTypes.func.isRequired,
      getCurrentPromises: PropTypes.func.isRequired,
      setCurrentRequests: PropTypes.func.isRequired,
      urlsForResource: PropTypes.func.isRequired,
    }).isRequired,
    deployments: PropTypes.arrayOf(PropTypes.object),
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.updateGraph = this.updateGraph.bind(this);

    this.state = {
      nodes: [],
      links: [],
      pendingRequests: false,
      error: ""
    };
  }

  componentDidMount() {
    this.svg = d3.select(".network-graph-container")
      .append("svg")
      .attr("class", "network-graph")
      .attr("width", defaultSvgWidth)
      .attr("height", defaultSvgHeight)
      .append("g")
      .attr("transform", "translate(" + margin.left + "," + margin.top + ")");

    this.loadFromServer();
  }

  componentDidUpdate() {
    simulation.alpha(1).restart();
  }

  componentWillUnmount() {
    this.api.cancelCurrentRequests();
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let deployments = _.sortBy(_.map(this.props.deployments, 'name'));
    let urls = _.map(this.props.deployments, d => {
      return this.api.fetchMetrics(this.api.urlsForResource("deployment", "") + "&to_name=" + d.name);
    });

    this.api.setCurrentRequests(urls);

    Promise.all(this.api.getCurrentPromises())
      .then(results => {
        let links = [];
        let nodeList = [];

        _.map(results, (r, i) => {
          let rows = _.get(r, ["ok", "statTables", 0, "podGroup", "rows"]);
          let dst = deployments[i];
          _.map(rows, row => {
            links.push({
              source: row.resource.name,
              target: dst,
              value: 1
            });
            nodeList.push(row.resource.name);
            nodeList.push(dst);
          });
        });

        let nodes = _.map(_.uniq(nodeList), n => {
          return { id: n, r: 15 };
        });

        this.setState({
          nodes: nodes,
          links: links,
          pendingRequests: false,
          error: ""
        });
      })
      .then( () => this.updateGraph())
      .catch(this.handleApiError);
  }

  updateGraph() {
    this.svg.append("svg:defs").selectAll("marker")
      .data(["end"])      // Different link/path types can be defined here
      .enter().append("svg:marker")    // This section adds in the arrows
      .attr("id", String)
      .attr("viewBox", "0 -5 10 10")
      .attr("refX", 18)
      .attr("refY", -0.25)
      .attr("markerWidth", 3)
      .attr("markerHeight", 3)
      .attr("orient", "auto")
      .append("svg:path")
      .attr("d", "M0,-5L10,0L0,5");

    // add the links and the arrows
    const path = this.svg.append("svg:g").selectAll("path")
      .data(this.state.links)
      .enter().append("svg:path")
      .attr("stroke-width", 3)
      .attr("stroke", "#000000")
      .attr("marker-end", "url(#end)");

    const nodeElements = this.svg.append('g')
      .selectAll('circle')
      .data(this.state.nodes)
      .enter().append('circle')
      .attr('r', 10)
      .attr('fill', 'steelblue')
      .call(d3.drag()
        .on("start", this.dragstarted)
        .on("drag", this.dragged)
        .on("end", this.dragended));

    const textElements = this.svg.append('g')
      .selectAll('text')
      .data(this.state.nodes)
      .enter().append('text')
      .text(node => node.id)
      .attr('font-size', 15)
      .attr('dx', 15)
      .attr('dy', 4);

    simulation.nodes(this.state.nodes).on("tick", () => {
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
      .links(this.state.links);
  }

  dragstarted(d) {
    if (!d3.event.active) {simulation.alphaTarget(0.3).restart();}
    d.fx = d.x;
    d.fy = d.y;
  }

  dragged(d) {
    d.fx = d3.event.x;
    d.fy = d3.event.y;
  }

  dragended(d) {
    if (!d3.event.active) {simulation.alphaTarget(0);}
    d.fx = null;
    d.fy = null;
  }

  handleApiError(e) {
    if (e.isCanceled) {
      return;
    }

    this.setState({
      pendingRequests: false,
      error: `Error getting data from server: ${e.message}`
    });
  }

  render() {
    return (
      <div>
        { !this.state.error ? null : <ErrorBanner message={this.state.error} /> }
        <div className="network-graph-container" />
      </div>
    );
  }
}

export default withContext(NetworkGraph);

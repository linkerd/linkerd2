import _ from 'lodash';
import { metricToFormatter } from './util/Utils.js';
import React from 'react';
import * as d3 from 'd3';
import './../../css/scatterplot.css';

const defaultSvgWidth = 574;
const defaultSvgHeight = 375;
const margin = { top: 0, right: 0, bottom: 10, left: 0 };
const circleRadius = 16;
const graphPadding = 3 * circleRadius;
const highlightBarWidth = 3 * circleRadius;
const successRateColorScale = d3.scaleQuantize()
  .domain([0, 1])
  .range(["#8B0000", "#FF6347", "#FF4500", "#FFA500","#008000"]);

const getP99 = d => _.get(d, ["latency", "P99", 0, "value"]);

export default class ScatterPlot extends React.Component {
  constructor(props) {
    super(props);
    this.renderNodeTooltipDatum = this.renderNodeTooltipDatum.bind(this);
    this.state = this.getChartDimensions();
  }

  componentWillMount() {
    this.initializeScales();
  }

  componentDidMount() {
    this.svg = d3.select("." + this.props.containerClassName)
      .append("svg")
      .attr("class", "scatterplot")
      .attr("width", defaultSvgWidth)
      .attr("height", defaultSvgHeight)
      .append("g")
      .attr("transform", "translate(" + margin.left + "," + margin.top + ")");

    this.xAxis = this.svg.append("g")
      .attr("class", "x-axis")
      .attr("transform", "translate(0," + (this.state.height - graphPadding) + ")");

    this.yAxis = this.svg.append("g")
      .attr("class", "y-axis")
      .attr("transform", "translate(" + this.state.width + ",0)");

    this.sidebar = d3.select(".scatterplot-display")
      .append("div").attr("class", "sidebar-tooltip");

    // overlay on which to attch mouse events
    this.overlay = this.svg.append("rect")
      .attr("transform", "translate(" + margin.left + "," + margin.top + ")")
      .attr("class", "overlay")
      .attr("width", this.state.width)
      .attr("height", this.state.height);

    this.initializeVerticalHighlight();
    this.renderAxisLabels();
    this.updateGraph();
  }

  shouldComponentUpdate(nextProps) {
    if (nextProps.lastUpdated === this.props.lastUpdated) {
      // control whether react re-renders the component
      // only rerender if the input data has changed
      return false;
    }
    return true;
  }

  componentDidUpdate() {
    this.updateGraph();
  }

  initializeVerticalHighlight() {
    this.updateScales(this.props.data);

    // highlight bar to show x position
    this.verticalHighlight = this.svg.append("rect")
      .attr("class", "vertical-highlight")
      .attr("width", highlightBarWidth)
      .attr("height", this.state.height);

    // when graph is initially loaded, set highlight and sidebar to first datapoint
    let firstDatapoint = _.first(this.props.data);
    if (firstDatapoint) {
      let firstLatency = _.get(firstDatapoint, ["latency", "P99", 0, "value"]);
      let firstLatencyX = this.xScale(firstLatency);
      let nearestDatapoints = this.getNearbyDatapoints(firstLatencyX, this.props.data);

      this.verticalHighlight
        .attr("transform", "translate(" + (firstLatencyX - (highlightBarWidth/2)) + ", 0)");
      this.renderSidebarTooltip(nearestDatapoints);
    }
  }

  getChartDimensions() {
    let svgWidth = this.props.width || defaultSvgWidth;
    let svgHeight = this.props.height || defaultSvgHeight;

    let width = svgWidth - margin.left - margin.right;
    let height = svgHeight - margin.top - margin.bottom;

    return {
      svgWidth: svgWidth,
      svgHeight: svgHeight,
      width: width,
      height: height,
      margin: margin
    };
  }

  initializeScales() {
    this.xScale = d3.scaleLinear().range([graphPadding, this.state.width - graphPadding]);
    this.yScale = d3.scaleLinear().range([this.state.height - graphPadding, graphPadding]);
  }

  updateScales(data) {
    this.xScale.domain(d3.extent(data, getP99));
    this.yScale.domain([0, 1]);

    this.updateAxes();
  }

  updateAxes() {
    let xAxis = d3.axisBottom(this.xScale)
      .ticks(5)
      .tickSize(5);
    this.xAxis.call(xAxis);

    let yAxis = d3.axisLeft(this.yScale)
      .ticks(4)
      .tickSize(this.state.width)
      .tickFormat(metricToFormatter["SUCCESS_RATE"]);

    // custom axis styling: https://bl.ocks.org/mbostock/3371592
    let customYAxis = g => {
      g.call(yAxis);
      g.select(".domain").remove();
      g.selectAll(".tick text")
        .attr("x", 4)
        .attr("dx", -10)
        .attr("dy", -4);
    };
    this.yAxis.call(customYAxis);
  }

  renderAxisLabels() {
    // text label for the x axis
    this.svg.append("text")
      .attr("class", "axis-label x-axis-label")
      .attr("y", this.state.height - 12)
      .attr("x", 0)
      .text("p99 Latency (ms)");

    // text label for the y axis
    this.svg.append("text")
      .attr("class", "axis-label y-axis-label")
      .attr("y", 20)
      .attr("x", this.state.width)
      .attr("dx", "-3em")
      .text("Success rate");
  }

  getNearbyDatapoints(x, data) {
    // return nodes that have nearby x-coordinates
    let xMargin = 30; // todo move constant
    let x0 = this.xScale.invert(x - xMargin);
    let x1 = this.xScale.invert(x + 2 * xMargin);

    return  _.filter(data, d => {
      let latency = getP99(d);
      return latency <= x1 && latency >= x0;
    });
  }

  updateGraph() {
    let plotData = this.props.data;

    this.scatterPlot = this.svg.selectAll(".dot")
      .data(plotData);

    this.scatterPlot.exit().remove();

    this.scatterPlot
      .enter()
      .append("circle")
      .attr("class", "dot")
      .attr("r", circleRadius)
      .merge(this.scatterPlot) // newfangled d3 'update' selection
      .attr("cx", d => this.xScale(getP99(d)))
      .attr("cy", d => this.yScale(d.successRate))
      .style("fill", d => successRateColorScale(d.successRate))
      .style("stroke", d => successRateColorScale(d.successRate));

    this.overlay
      .on("mousemove", () => {
        let currXPos = d3.mouse(d3.select("rect").node())[0];
        this.verticalHighlight.attr("transform", "translate(" + currXPos + ", 0)");
        let nearestDatapoints = this.getNearbyDatapoints(currXPos, plotData);

        this.renderSidebarTooltip(nearestDatapoints);
      });
  }

  renderSidebarTooltip(data) {
    let innerHtml = _(data)
      .orderBy('successRate', 'desc')
      .map(d => this.renderNodeTooltipDatum(d)).value();
    this.sidebar.html(innerHtml.join(''));
  }

  renderNodeTooltipDatum(d) {
    let latency = metricToFormatter["LATENCY"](getP99(d));
    let sr = metricToFormatter["SUCCESS_RATE"](d.successRate);
    return `<div class="title">${d.name}</div><div>${latency}, ${sr}</div>`;
  }

  render() {
    // d3 selects the passed in container from this.props.containerClassName
    return _.isEmpty(this.props.data) ?
      <div className="clearfix no-data-msg">No data</div> : null;
  }
}

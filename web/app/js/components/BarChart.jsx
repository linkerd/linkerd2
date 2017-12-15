import _ from 'lodash';
import { metricToFormatter } from './util/Utils.js';
import Percentage from './util/Percentage.js';
import React from 'react';
import * as d3 from 'd3';
import './../../css/bar-chart.css';

const defaultSvgWidth = 595;
const defaultSvgHeight = 150;
const margin = { top: 0, right: 0, bottom: 20, left: 0 };
const horizontalLabelLimit = 4; // number of bars beyond which to tilt axis labels
const labelLimit = 40; // beyond this, stop labelling bars entirely

export default class LineGraph extends React.Component {
  constructor(props) {
    super(props);

    this.state = this.getChartDimensions();
  }

  componentWillMount() {
    this.initializeScales();
  }

  componentDidMount() {
    this.svg = d3.select("." + this.props.containerClassName)
      .append("svg")
      .attr("class", "bar-chart")
      .attr("width", this.state.svgWidth)
      .attr("height", this.state.svgHeight)
      .append("g")
      .attr("transform", "translate(" + this.state.margin.left + "," + this.state.margin.top + ")");

    this.tooltip = d3.select("." + this.props.containerClassName + " .bar-chart-tooltip")
      .append("div").attr("class", "tooltip");

    this.xAxis = this.svg.append("g")
      .attr("class", "x-axis")
      .attr("transform", "translate(0," + this.state.height + ")");

    this.updateScales();
    this.renderGraph();
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
    this.updateScales();
    this.renderGraph();
  }

  getChartDimensions() {
    let svgWidth = this.props.width || defaultSvgWidth;
    let svgHeight = this.props.height || defaultSvgHeight;
    let tiltLabels = false;
    let hideLabels = false;

    if (_.size(this.props.data) > horizontalLabelLimit) {
      if(_.size(this.props.data) > labelLimit) {
        // if there are way too many bars, don't label at all
        hideLabels = true;
      } else {
        // if there are many bars, tilt x axis labels
        margin.bottom += 100;
        tiltLabels = true;
      }
    }

    let width = svgWidth - margin.left - margin.right;
    let height = svgHeight - margin.top - margin.bottom;

    return {
      svgWidth: svgWidth,
      svgHeight: svgHeight,
      width: width,
      height: height,
      margin: margin,
      tiltLabels: tiltLabels,
      hideLabels: hideLabels
    };
  }

  chartData() {
    let data = this.props.data;
    _.each(data, d => {
      let p = new Percentage(d.requestRate, d.totalRequests);
      d.shareOfRequests = p.get();
      d.pretty = p.prettyRate();
    });
    return data;
  }

  updateScales() {
    let data = this.chartData();
    this.xScale.domain(_.map(data, d => d.name));
    this.yScale.domain([0, d3.max(data, d => d.requestRate)]);
  }

  initializeScales() {
    this.xScale = d3.scaleBand()
      .range([0, this.state.width])
      .padding(0.1);
    this.yScale = d3.scaleLinear()
      .range([this.state.height, 0]);
  }

  renderGraph() {
    let data = this.chartData();
    let barChart = this.svg.selectAll(".bar")
      .remove()
      .exit()
      .data(data);

    barChart.enter().append("rect")
      .attr("class", "bar")
      .attr("x", d => this.xScale(d.name))
      .attr("width", () =>  this.xScale.bandwidth())
      .attr("y", d => this.yScale(d.requestRate))
      .attr("height", d => this.state.height - this.yScale(d.requestRate))
      .on("mousemove", d => {
        this.tooltip
          .style("left", d3.event.pageX - 50 + "px")
          .style("top", d3.event.pageY - 70 + "px")
          .style("display", "inline-block") // show tooltip
          .html(`${d.name}:<br /> ${metricToFormatter["REQUEST_RATE"](d.requestRate)} (${d.pretty} of total)`);
      })
      .on("mouseout", () => this.tooltip.style("display", "none"));

    this.updateAxes();
  }

  updateAxes() {
    this.xAxis
      .call(d3.axisBottom(this.xScale)) // add x axis labels
      .selectAll("text")
      .attr("class", "tick-labels")
      .attr("transform", () => this.state.tiltLabels ? "rotate(-65)" : "")
      .style("text-anchor", () => this.state.tiltLabels ? "end" : "")
      .text(d => {
        if (this.state.hideLabels) {
          return;
        }

        let displayText = d;
        // truncate long label names
        if (_.size(displayText) > 20) {
          displayText = "..." + displayText.substring(displayText.length - 20);
        }
        return displayText;
      });
  }

  render() {
    return null;
  }
}

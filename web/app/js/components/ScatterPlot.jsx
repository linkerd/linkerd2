import React from 'react';
import * as d3 from 'd3';
import { metricToFormatter } from './util/Utils.js';
import styles from './../../css/scatterplot.css';

const defaultSvgWidth = 906;
const defaultSvgHeight = 375;
const margin = { top: 10, right: 0, bottom: 10, left: 10 };
const circleRadius = 16;
const graphPadding = 3 * circleRadius;
const successRateColorScale = d3.scaleLinear()
  .domain([0, 1])
  .range(["#FF9292", "#addd8e"]);
const successRateStrokeColorScale = d3.scaleLinear()
  .domain([0, 1])
  .range(["#EB5757", "#31a354"]);

export default class ScatterPlot extends React.Component {
  constructor(props) {
    super(props);
    this.state = this.getChartDimensions();
  }

  shouldComponentUpdate(nextProps, nextState) {
    if (nextProps.lastUpdated === this.props.lastUpdated) {
      // control whether react re-renders the component
      // only rerender if the input data has changed
      return false;
    }
    return true;
  }

  componentWillMount() {
    this.initializeScales();
  }

  componentDidUpdate() {
    this.updateGraph();
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
    }
  }

  initializeScales() {
    this.xScale = d3.scaleLinear().range([graphPadding, this.state.width - graphPadding]);
    this.yScale = d3.scaleLinear().range([this.state.height - graphPadding, graphPadding]);
  }

  updateScales(data) {
    this.xScale.domain(d3.extent(data, d => d.latency));
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
    }
    this.yAxis.call(customYAxis);
  }

  componentDidMount() {
    this.svg = d3.select("." + this.props.containerClassName)
      .append("svg")
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

    this.tooltip = d3.select("." + this.props.containerClassName + " .scatterplot-tooltip")
      .append("div").attr("class", "tooltip");

    this.renderAxisLabels();
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

  updateGraph() {
    let plotData = _.reduce(this.props.data, (mem, datum) => {
      if(!_.isNil(datum.scatterplot.success) && !_.isNil(datum.scatterplot.latency)) {
        mem.push(datum.scatterplot);
      }
      return mem;
    }, []);
    this.updateScales(plotData);
    this.scatterPlot = this.svg.selectAll(".dot")
      .data(plotData)

    this.labels = this.svg.selectAll(".dot-label")
      .data(plotData)

    this.scatterPlot
      .enter()
        .append("circle")
        .attr("class", "dot")
        .attr("r", circleRadius)
      .merge(this.scatterPlot) // newfangled d3 'update' selection
        .attr("cx", d => this.xScale(d.latency))
        .attr("cy", d => this.yScale(d.success))
        .style("fill", d => successRateColorScale(d.success))
        .style("stroke", d => successRateStrokeColorScale(d.success))
        .on("mousemove", d => {
          let sr = metricToFormatter["SUCCESS_RATE"](d.success);
          let latency = metricToFormatter["LATENCY"](d.latency);
          this.tooltip
            .style("left", d3.event.pageX - 50 + "px")
            .style("top", d3.event.pageY - 70 + "px")
            .style("display", "inline-block") // show tooltip
            .text(`${d.label}: (${latency}, ${sr})`);
        })
        .on("mouseout", () => this.tooltip.style("display", "none"));

    this.labels
      .enter()
        .append("text")
        .attr("class", "dot-label")
      .merge(this.labels)
        .text(d => d.label)
        .attr("x", d => this.xScale(d.latency) - circleRadius)
        .attr("y", d => this.yScale(d.success) - 2 * circleRadius)
  }

  render() {
    // d3 selects the passed in container from this.props.containerClassName
    return _.isEmpty(this.props.data) ?
      <div className="clearfix no-data-msg">No data</div> : null;
  }
}

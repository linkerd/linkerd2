import { Plural, Trans } from '@lingui/macro';
import PropTypes from 'prop-types';
import React from 'react';
import Step from '@material-ui/core/Step';
import StepContent from '@material-ui/core/StepContent';
import StepLabel from '@material-ui/core/StepLabel';
import Stepper from '@material-ui/core/Stepper';
import Typography from '@material-ui/core/Typography';
import { incompleteMeshMessage } from './util/CopyUtils.jsx';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  instructions: {
    marginTop: theme.spacing(1),
    marginBottom: theme.spacing(1),
  },
});

function getSteps(numResources, resource) {
  return [
    { label: <Trans>controllerInstalledMsg</Trans>, key: 'installedMsg' },
    { label: <Plural value={numResources} zero={resource} one={resource} other={resource} />, key: 'resourceMsg' },
    { label: <Trans>connectResourceMsg {resource}</Trans>, content: incompleteMeshMessage(), key: 'connectMsg' },
  ];
}

const CallToAction = ({ resource, numResources, classes }) => {
  const steps = getSteps(numResources, resource);
  const lastStep = steps.length - 1; // hardcode the last step as the active step

  return (
    <React.Fragment>
      <Typography><Trans>serviceMeshInstalledMsg</Trans></Typography>
      <Stepper
        activeStep={lastStep}
        className={classes.instructions}
        orientation="vertical">
        {
          steps.map((step, i) => {
            const props = {};
            props.completed = i < lastStep; // select the last step as the currently active one

            return (
              <Step key={step.key} {...props}>
                <StepLabel>{step.label}</StepLabel>
                <StepContent>{step.content}</StepContent>
              </Step>
            );
          })
        }
      </Stepper>
    </React.Fragment>
  );
};

CallToAction.propTypes = {
  numResources: PropTypes.number,
  resource: PropTypes.string.isRequired,
};

CallToAction.defaultProps = {
  numResources: null,
};

export default withStyles(styles)(CallToAction);
